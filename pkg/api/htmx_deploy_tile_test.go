package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"tricms/pkg/storage"
)

func TestDeployTileView(t *testing.T) {
	past := func(d time.Duration) *time.Time { t := time.Now().Add(-d); return &t }

	cases := []struct {
		name      string
		delivery  *storage.WebhookDelivery
		wantNil   bool
		wantClass string
	}{
		{"nil delivery -> nothing", nil, true, ""},
		{"uncorrelated (empty status) shows as in progress", &storage.WebhookDelivery{DeployStatus: ""}, false, "tri-deploy-tile-warning"},
		{"queued shows as in progress", &storage.WebhookDelivery{DeployStatus: "queued"}, false, "tri-deploy-tile-warning"},
		{"in_progress shows as in progress", &storage.WebhookDelivery{DeployStatus: "in_progress"}, false, "tri-deploy-tile-warning"},
		{
			"completed/success within grace period shows success",
			&storage.WebhookDelivery{DeployStatus: "completed", DeployConclusion: "success", DeployResolvedAt: past(5 * time.Second)},
			false, "tri-deploy-tile-success",
		},
		{
			"completed/failure within grace period shows danger",
			&storage.WebhookDelivery{DeployStatus: "completed", DeployConclusion: "failure", DeployResolvedAt: past(5 * time.Second)},
			false, "tri-deploy-tile-danger",
		},
		{
			"completed/success past the 30s grace period shows nothing",
			&storage.WebhookDelivery{DeployStatus: "completed", DeployConclusion: "success", DeployResolvedAt: past(45 * time.Second)},
			true, "",
		},
		{
			"completed with no DeployResolvedAt (shouldn't happen, but) shows nothing",
			&storage.WebhookDelivery{DeployStatus: "completed", DeployConclusion: "success"},
			true, "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deployTileView(c.delivery)
			if c.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a non-nil tile view")
			}
			if got.Class != c.wantClass {
				t.Fatalf("expected class %q, got %q", c.wantClass, got.Class)
			}
		})
	}
}

// TestHTMX_DeployTile_EndToEnd drives the actual HTTP handler (not just
// deployTileView) across the lifecycle: nothing to show, in-progress,
// resolved-and-visible, resolved-and-expired. Reads real rendered HTML, and
// exercises the exact same storage query (LatestGitHubDispatchDelivery) the
// handler uses.
func TestHTMX_DeployTile_EndToEnd(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("tile@x.com", true)
	p := e.createProject("TileCheck")

	// No github_dispatch delivery at all yet: empty response.
	rec := e.getHTML("/projects/"+p.ID+"/deploy-tile", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("expected an empty tile with no delivery yet, got: %s", rec.Body.String())
	}

	createResp := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", GitHubRepo: "mon-site", GitHubToken: "ghp_secret",
		Events: []string{"content.publish"},
	})
	wh := decodeBody[storage.Webhook](t, createResp)
	if err := e.server.System.RecordWebhookDelivery(bgCtx(), &storage.WebhookDelivery{
		WebhookID: wh.ID, ProjectID: p.ID, Event: "content.publish", Success: true, Attempts: 1, StatusCode: 204,
	}); err != nil {
		t.Fatalf("record delivery: %v", err)
	}

	// Dispatch accepted, not correlated yet: shown as in-progress.
	rec = e.getHTML("/projects/"+p.ID+"/deploy-tile", admin)
	if !strings.Contains(rec.Body.String(), "Déploiement en cours") || !strings.Contains(rec.Body.String(), "tri-deploy-tile-warning") {
		t.Fatalf("expected an in-progress tile, got: %s", rec.Body.String())
	}

	deliveries, _ := e.server.System.ListWebhookDeliveries(bgCtx(), p.ID, 10)
	if err := e.server.System.UpdateWebhookDeliveryDeployState(bgCtx(), deliveries[0].ID, 1, "completed", "success", "https://github.com/louis/mon-site/actions/runs/1"); err != nil {
		t.Fatalf("update deploy state: %v", err)
	}

	// Just resolved: visible with the success state.
	rec = e.getHTML("/projects/"+p.ID+"/deploy-tile", admin)
	body := rec.Body.String()
	if !strings.Contains(body, "Déploiement réussi") || !strings.Contains(body, "tri-deploy-tile-success") {
		t.Fatalf("expected a success tile, got: %s", body)
	}
	// The site-wide tile is a lightweight glanceable status, no GitHub link --
	// the full run link still lives in the Webhooks page's delivery history.
	if strings.Contains(body, "href=") {
		t.Fatalf("expected no run link in the site-wide tile, got: %s", body)
	}

	// Backdate the resolution past the 30s grace window directly via
	// storage (waiting 30 real seconds in a test would be silly).
	if _, err := e.server.System.DB().ExecContext(bgCtx(),
		`UPDATE webhook_deliveries SET deploy_resolved_at = datetime('now', '-45 seconds') WHERE id = ?`, deliveries[0].ID); err != nil {
		t.Fatalf("backdate resolution: %v", err)
	}
	rec = e.getHTML("/projects/"+p.ID+"/deploy-tile", admin)
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("expected the tile to disappear past the grace period, got: %s", rec.Body.String())
	}
}

// TestHTMX_DeployTile_AnyProjectMemberCanSeeIt confirms the tile is gated
// by mere project membership (loadProjectForHTMXAny), not the ADMIN-only
// SectionWebhooks role required to manage webhooks themselves -- deploy
// feedback for a publish is relevant to whoever published it.
func TestHTMX_DeployTile_AnyProjectMemberCanSeeIt(t *testing.T) {
	e := newHTMXTestEnv(t)
	redacteur := e.createUser("redacteur-tile@x.com", false)
	p := e.createProject("TileAccess")
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	rec := e.getHTML("/projects/"+p.ID+"/deploy-tile", redacteur)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected a REDACTEUR to see the deploy tile endpoint (200), got %d: %s", rec.Code, rec.Body.String())
	}

	outsider := e.createUser("outsider-tile@x.com", false)
	rec = e.getHTML("/projects/"+p.ID+"/deploy-tile", outsider)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a user with no role on the project, got %d", rec.Code)
	}
}

// TestHTMX_Topbar_EmbedsDeployTileOnlyForProjectPages confirms the polled
// #deploy-tile div is injected on project pages (via partial:topbar) but
// not on pages with no current project (the dashboard).
func TestHTMX_Topbar_EmbedsDeployTileOnlyForProjectPages(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("topbartile@x.com", true)
	p := e.createProject("TopbarTile")

	rec := e.getHTML("/projects/"+p.ID, admin)
	if !strings.Contains(rec.Body.String(), `id="deploy-tile"`) {
		t.Fatalf("expected the deploy tile slot on a project page, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `hx-get="/projects/`+p.ID+`/deploy-tile"`) {
		t.Fatalf("expected the deploy tile to poll the right project, got: %s", rec.Body.String())
	}

	rec = e.getHTML("/", admin)
	if strings.Contains(rec.Body.String(), `id="deploy-tile"`) {
		t.Fatalf("expected no deploy tile slot on the dashboard (no current project), got: %s", rec.Body.String())
	}
}
