package api

import (
	"log"
	"net/http"
	"time"

	"tricms/pkg/storage"
)

// deployTileGracePeriod is how long the topbar deploy tile keeps showing a
// resolved deploy's outcome before disappearing -- long enough to actually
// notice it landed, short enough not to linger as stale information.
const deployTileGracePeriod = 30 * time.Second

// deployTileView computes what the topbar deploy tile should show for a
// project's most recent github_dispatch delivery, or nil to show nothing.
//
// Unlike deployStatusBadge (the Webhooks page's delivery-history table,
// where an empty DeployStatus means "not correlated yet, show a neutral
// placeholder"), an empty status here is shown the same as "in_progress":
// from the person who just published's perspective, the deploy already
// started the moment they published, whether or not the background poller
// (RunDeployStatusPoller) has found the exact GitHub run yet.
func deployTileView(d *storage.WebhookDelivery) *deployBadge {
	if d == nil {
		return nil
	}
	if !d.DeployFinal() {
		return &deployBadge{Label: "Déploiement en cours…", Class: "tri-deploy-tile-warning", Icon: "hourglass_top", URL: d.DeployRunURL}
	}
	if !d.DeployRecentlyResolved(deployTileGracePeriod) {
		return nil // resolved a while ago -- stop showing it
	}
	switch d.DeployConclusion {
	case "success":
		return &deployBadge{Label: "Déploiement réussi", Class: "tri-deploy-tile-success", Icon: "check_circle", URL: d.DeployRunURL}
	case "failure", "timed_out":
		return &deployBadge{Label: "Échec du déploiement", Class: "tri-deploy-tile-danger", Icon: "error", URL: d.DeployRunURL}
	default: // cancelled, action_required, stale, skipped, neutral, or a future value we don't special-case
		label := d.DeployConclusion
		if label == "" {
			label = "Déploiement terminé"
		}
		return &deployBadge{Label: label, Class: "tri-deploy-tile-warning", Icon: "info", URL: d.DeployRunURL}
	}
}

// htmxDeployTile renders the site-wide deploy-status tile shown at the top
// of every project page (see partial:topbar's polled #deploy-tile div).
// Purely a database read -- same as htmxWebhooks -- never calls the GitHub
// API itself; see RunDeployStatusPoller (deploy_poller.go) for the write
// side of this deliberately decoupled design.
//
// Gated by loadProjectForHTMXAny rather than SectionWebhooks: this is
// build/deploy feedback for content someone just published, relevant to
// any project member, not just whoever manages the webhook itself.
//
// Renders an empty body when there's nothing to show, which is the normal
// case -- the polled #deploy-tile div's innerHTML just goes empty and it
// collapses to zero height, no separate "hide" signal needed.
func (s *Server) htmxDeployTile(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMXAny(w, r)
	if user == nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	d, err := s.System.LatestGitHubDispatchDelivery(r.Context(), project.ID)
	if err != nil {
		return // decorative feedback, not core functionality -- fail silent
	}
	view := deployTileView(d)
	if view == nil {
		// Nothing to show, and nothing left that could change it on its own:
		// either there's never been a delivery, or the last one is done and
		// past the grace period. Either way a future delivery only ever
		// starts from a fresh page load (publishing is a plain form POST,
		// not an in-page action), so there's no reason for the topbar tile
		// to keep polling every 5s forever. 286 is htmx's documented "stop
		// polling" response code -- the swap (here, an empty body) still
		// happens, but the element's poll timer is cancelled.
		w.WriteHeader(286)
		return
	}
	if err := s.Templates.ExecuteTemplate(w, "partial:deploy_tile", view); err != nil {
		log.Printf("template render error (partial:deploy_tile): %v", err)
	}
}
