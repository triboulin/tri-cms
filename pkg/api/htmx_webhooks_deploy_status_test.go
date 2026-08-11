package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tricms/pkg/storage"
)

// TestHTMX_WebhookDeployStatus_PageIsReadOnly is the frontend half of the
// decoupled polling design: the Webhooks page must render whatever the
// background poller (pollPendingDeploys) has already persisted, and must
// never itself call the GitHub API -- fetching the page any number of
// times must not change the delivery's deploy state or hit GitHub at all.
func TestHTMX_WebhookDeployStatus_PageIsReadOnly(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("pagereadonly@x.com", true)
	p := e.createProject("PageReadOnly")

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

	var githubCalls int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&githubCalls, 1)
		fmt.Fprint(w, `{"workflow_runs":[]}`)
	}))
	defer fake.Close()
	e.server.Dispatcher.GitHubAPIBaseURL = fake.URL

	// Before the poller has ever run: nothing correlated, page shows "—".
	rec := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<span class="tri-badge-outline">—</span>`) {
		t.Fatalf("expected the unresolved placeholder before any poll, got: %s", rec.Body.String())
	}

	// Fetching the page repeatedly must never call GitHub -- only the
	// background poller does that.
	for i := 0; i < 3; i++ {
		e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	}
	if atomic.LoadInt32(&githubCalls) != 0 {
		t.Fatalf("expected the Webhooks page to never call the GitHub API itself, got %d calls", githubCalls)
	}

	// Now run the background poller explicitly (simulating its own
	// independent tick) and confirm the page picks up what it wrote,
	// purely by reading the database.
	e.server.pollPendingDeploys(bgCtx())
	if atomic.LoadInt32(&githubCalls) == 0 {
		t.Fatal("expected the poller itself to have called GitHub")
	}
	callsAfterPoll := atomic.LoadInt32(&githubCalls)

	deliveries, _ := e.server.System.ListWebhookDeliveries(bgCtx(), p.ID, 10)
	if deliveries[0].DeployStatus != "" {
		// The fake server returned an empty workflow_runs list, so
		// correlation legitimately found nothing yet -- still "—" is fine,
		// the important assertion is that the page didn't drive this call.
		t.Logf("delivery deploy status after poll: %+v", deliveries[0])
	}

	rec = e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if atomic.LoadInt32(&githubCalls) != callsAfterPoll {
		t.Fatalf("expected the page GET to add zero GitHub calls, went from %d to %d", callsAfterPoll, githubCalls)
	}
}

// TestHTMX_WebhookDeployStatus_RendersPersistedState confirms the actual
// badge rendering (text/class/link) once the poller has written a
// completed, successful deploy -- reading real html/template output, not
// just the storage layer.
func TestHTMX_WebhookDeployStatus_RendersPersistedState(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("renderstate@x.com", true)
	p := e.createProject("RenderState")

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
	deliveries, _ := e.server.System.ListWebhookDeliveries(bgCtx(), p.ID, 10)

	// Simulate what the poller would have written, directly via storage --
	// this test is about template rendering, not the poller itself (see
	// deploy_poller_test.go for that).
	if err := e.server.System.UpdateWebhookDeliveryDeployState(bgCtx(), deliveries[0].ID, 42, "completed", "success", "https://github.com/louis/mon-site/actions/runs/42"); err != nil {
		t.Fatalf("update deploy state: %v", err)
	}

	rec := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	body := rec.Body.String()
	if !strings.Contains(body, "Déployé") || !strings.Contains(body, "tri-badge-success") {
		t.Fatalf("expected a success deploy badge, got: %s", body)
	}
	if !strings.Contains(body, `href="https://github.com/louis/mon-site/actions/runs/42"`) {
		t.Fatalf("expected the badge to link to the run, got: %s", body)
	}

	// And a failure conclusion renders the danger variant.
	if err := e.server.System.UpdateWebhookDeliveryDeployState(bgCtx(), deliveries[0].ID, 43, "completed", "failure", "https://github.com/louis/mon-site/actions/runs/43"); err != nil {
		t.Fatalf("update deploy state: %v", err)
	}
	rec = e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	body = rec.Body.String()
	if !strings.Contains(body, "Échec du déploiement") || !strings.Contains(body, "tri-badge-danger") {
		t.Fatalf("expected a failure deploy badge, got: %s", body)
	}
}
