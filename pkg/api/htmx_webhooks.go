package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

// availableWebhookEvents lists every event a webhook can subscribe to, for
// the create/edit form's checkbox list.
func availableWebhookEvents() []string {
	return []string{
		webhooks.EventContentCreate, webhooks.EventContentUpdate, webhooks.EventContentDelete,
		webhooks.EventContentPublish, webhooks.EventContentUnpublish,
		webhooks.EventMediaCreate, webhooks.EventMediaDelete,
	}
}

type webhookEventOption struct {
	Value    string
	Selected bool
}

func webhookEventOptions(selected []string) []webhookEventOption {
	set := map[string]bool{}
	for _, e := range selected {
		set[e] = true
	}
	opts := make([]webhookEventOption, 0)
	for _, e := range availableWebhookEvents() {
		opts = append(opts, webhookEventOption{Value: e, Selected: set[e]})
	}
	return opts
}

// webhookDeliveryVM adds a human-readable label (kind + target) to a raw
// storage.WebhookDelivery for display, since the history can outlive the
// webhook it refers to (deleted webhooks still show up by ID).
type webhookDeliveryVM struct {
	WebhookLabel string
	Delivery     *storage.WebhookDelivery
	DeployBadge  *deployBadge // nil when not a github_dispatch delivery, or not correlated to a run yet
}

// deployBadge describes how to render a github_dispatch delivery's
// downstream build/deploy state (as opposed to Delivery.Success, which only
// means "GitHub accepted the dispatch call") in the history table.
type deployBadge struct {
	Label string
	Class string // one of the .tri-badge-* modifiers
	URL   string // link to the GitHub Actions run; empty until correlated
}

func deployStatusBadge(d *storage.WebhookDelivery) *deployBadge {
	switch d.DeployStatus {
	case "":
		return nil
	case "queued":
		return &deployBadge{Label: "Déploiement en attente", Class: "tri-badge-warning", URL: d.DeployRunURL}
	case "in_progress":
		return &deployBadge{Label: "Déploiement en cours", Class: "tri-badge-warning", URL: d.DeployRunURL}
	case "completed":
		switch d.DeployConclusion {
		case "success":
			return &deployBadge{Label: "Déployé", Class: "tri-badge-success", URL: d.DeployRunURL}
		case "failure", "timed_out":
			return &deployBadge{Label: "Échec du déploiement", Class: "tri-badge-danger", URL: d.DeployRunURL}
		default: // cancelled, action_required, stale, skipped, neutral, or a future value we don't special-case
			label := d.DeployConclusion
			if label == "" {
				label = "Terminé"
			}
			return &deployBadge{Label: label, Class: "tri-badge-outline", URL: d.DeployRunURL}
		}
	default:
		return &deployBadge{Label: d.DeployStatus, Class: "tri-badge-outline", URL: d.DeployRunURL}
	}
}

// reconcileGitHubDispatchDeploys best-effort correlates and polls the
// downstream GitHub Actions run for recent kind=github_dispatch deliveries
// whose deploy state isn't final yet (see storage.WebhookDelivery.DeployFinal),
// so the Webhooks page can show the real build/deploy outcome instead of
// just "GitHub accepted the dispatch call". Client-initiated polling: this
// only ever runs because a browser has the Webhooks page open and its 5s
// HTMX auto-refresh (see webhooks.html) hit this handler again -- there is
// no server-side background poller, so nothing happens while nobody's
// looking.
//
// Bounded on two axes so a slow/unreachable GitHub API can't stall the
// page: a short overall time budget, and a cap on how many deliveries get
// checked per page load (in normal use there's at most one delivery
// in-flight at a time; the cap just protects against a burst). Any GitHub
// API error -- including a 401/403 from a token that hasn't been granted
// the Actions: Read permission yet -- is swallowed here: the delivery's
// deploy state simply stays whatever it already was, tried again on the
// next poll, never surfaced as a page error.
func (s *Server) reconcileGitHubDispatchDeploys(ctx context.Context, whByID map[string]*storage.Webhook, deliveries []*storage.WebhookDelivery) {
	if s.Dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	const maxReconcilePerLoad = 5
	checked := 0
	for _, d := range deliveries {
		if checked >= maxReconcilePerLoad || ctx.Err() != nil {
			return
		}
		if !d.Success || d.DeployFinal() {
			continue // the dispatch call itself failed, or this is already resolved
		}
		wh, ok := whByID[d.WebhookID]
		if !ok || wh.Kind != webhooks.KindGitHubDispatch {
			continue
		}
		var cfg webhooks.GitHubDispatchConfig
		if err := json.Unmarshal([]byte(wh.Config), &cfg); err != nil {
			continue
		}
		checked++

		var run *webhooks.RunInfo
		var err error
		if d.DeployRunID != 0 {
			run, err = s.Dispatcher.GetRun(ctx, cfg.Owner, cfg.Repo, cfg.Token, d.DeployRunID)
		} else {
			run, err = s.Dispatcher.FindDispatchRun(ctx, cfg.Owner, cfg.Repo, cfg.Token, d.CreatedAt)
		}
		if err != nil || run == nil {
			continue // couldn't tell right now, or GitHub hasn't queued the run yet -- retry next poll
		}
		if err := s.System.UpdateWebhookDeliveryDeployState(ctx, d.ID, run.ID, run.Status, run.Conclusion, run.HTMLURL); err != nil {
			continue
		}
		d.DeployRunID, d.DeployStatus, d.DeployConclusion, d.DeployRunURL = run.ID, run.Status, run.Conclusion, run.HTMLURL
	}
}

type webhookRowVM struct {
	Webhook     *storage.Webhook
	Events      []webhookEventOption
	IsGitHub    bool
	GitHubOwner string
	GitHubRepo  string
}

// githubDispatchDisplayFields extracts the non-secret parts (owner/repo) of
// a github_dispatch webhook's Config, for prefilling the edit form -- the
// token itself is never sent back to the browser, only re-entered to
// rotate it (see htmxUpdateWebhook).
func githubDispatchDisplayFields(wh *storage.Webhook) (owner, repo string) {
	if wh.Kind != webhooks.KindGitHubDispatch || wh.Config == "" {
		return "", ""
	}
	var cfg webhooks.GitHubDispatchConfig
	_ = json.Unmarshal([]byte(wh.Config), &cfg)
	return cfg.Owner, cfg.Repo
}

// htmxWebhooks renders the webhook management page (spec §4.2 "[Webhooks]",
// global-admin only). This page previously didn't exist even though the
// sidebar linked to it.
func (s *Server) htmxWebhooks(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	whs, err := s.System.ListWebhooks(r.Context(), project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	whByID := make(map[string]*storage.Webhook, len(whs))
	rows := make([]webhookRowVM, 0, len(whs))
	for _, wh := range whs {
		whByID[wh.ID] = wh
		owner, repo := githubDispatchDisplayFields(wh)
		rows = append(rows, webhookRowVM{
			Webhook:     wh,
			Events:      webhookEventOptions(wh.Events),
			IsGitHub:    wh.Kind == webhooks.KindGitHubDispatch,
			GitHubOwner: owner,
			GitHubRepo:  repo,
		})
	}

	// Delivery history (spec: debug tooling for "why isn't my webhook
	// firing" -- previously the only trace of a delivery was a failure
	// logged in the generic, project-wide /logs page, with no way to tell
	// "no webhook is subscribed to this event" apart from "it's failing"
	// apart from "it's actually working").
	deliveries, err := s.System.ListWebhookDeliveries(r.Context(), project.ID, 50)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	s.reconcileGitHubDispatchDeploys(r.Context(), whByID, deliveries)
	deliveryRows := make([]webhookDeliveryVM, 0, len(deliveries))
	for _, d := range deliveries {
		label := d.WebhookID
		if wh, ok := whByID[d.WebhookID]; ok {
			label = wh.Kind + " " + wh.URL
			if wh.Kind == webhooks.KindGitHubDispatch {
				owner, repo := githubDispatchDisplayFields(wh)
				label = "github_dispatch " + owner + "/" + repo
			}
		}
		deliveryRows = append(deliveryRows, webhookDeliveryVM{
			WebhookLabel: label,
			Delivery:     d,
			DeployBadge:  deployStatusBadge(d),
		})
	}

	content := struct {
		Webhooks        []webhookRowVM
		NewEventOptions []webhookEventOption
		Deliveries      []webhookDeliveryVM
	}{rows, webhookEventOptions(availableWebhookEvents()), deliveryRows}

	data, err := s.buildPageData(r.Context(), user, project, "webhooks", "", content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:webhooks", data)
}

// webhookRequestFromForm reads a submitted create/edit webhook form into a
// webhookRequest, so both HTMX handlers below can share the same
// kind-aware validation/encryption logic as the JSON API
// (Server.buildWebhookFields in handlers_webhooks.go).
func webhookRequestFromForm(r *http.Request) webhookRequest {
	return webhookRequest{
		Kind:        r.FormValue("kind"),
		URL:         r.FormValue("url"),
		Secret:      r.FormValue("secret"),
		Events:      r.Form["events"],
		GitHubOwner: r.FormValue("github_owner"),
		GitHubRepo:  r.FormValue("github_repo"),
		GitHubToken: r.FormValue("github_token"),
	}
}

func (s *Server) htmxCreateWebhook(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/webhooks"
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	req := webhookRequestFromForm(r)
	kind, url, secret, config, problem := s.buildWebhookFields(&req, nil)
	if problem != "" {
		redirectWithFlash(w, r, back, problem, "error")
		return
	}
	wh := &storage.Webhook{ID: uuid.NewString(), ProjectID: project.ID, Kind: kind, URL: url, Secret: secret, Config: config, Events: req.Events}
	if err := s.System.CreateWebhook(r.Context(), wh); err != nil {
		redirectWithFlash(w, r, back, "Création impossible : "+err.Error(), "error")
		return
	}
	redirectWithFlash(w, r, back, "Webhook créé.", "success")
}

func (s *Server) htmxUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/webhooks"
	webhookID := chi.URLParam(r, "webhookID")
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	existing, err := s.System.GetWebhook(r.Context(), webhookID)
	if err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	req := webhookRequestFromForm(r)
	kind, url, secret, config, problem := s.buildWebhookFields(&req, existing)
	if problem != "" {
		redirectWithFlash(w, r, back, problem, "error")
		return
	}
	wh := &storage.Webhook{ID: webhookID, Kind: kind, URL: url, Secret: secret, Config: config, Events: req.Events}
	if err := s.System.UpdateWebhook(r.Context(), wh); err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	redirectWithFlash(w, r, back, "Webhook mis à jour.", "success")
}

func (s *Server) htmxDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/webhooks"
	webhookID := chi.URLParam(r, "webhookID")
	if err := s.System.DeleteWebhook(r.Context(), webhookID); err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	redirectWithFlash(w, r, back, "Webhook supprimé.", "success")
}
