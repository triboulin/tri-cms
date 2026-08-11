package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tricms/pkg/storage"
)

// TestHTMX_WebhookDeployStatus_ReconciliationLifecycle exercises the full
// client-initiated-polling loop for a github_dispatch delivery's downstream
// build/deploy status: the Webhooks page's periodic HTMX refresh
// (hx-trigger="every 5s" in webhooks.html) hits htmxWebhooks each time,
// which best-effort correlates/polls the GitHub Actions API
// (reconcileGitHubDispatchDeploys) and persists whatever it learns, so a
// human watching the page sees the state progress in_progress -> success --
// and, crucially, GitHub stops getting polled once the run is final.
func TestHTMX_WebhookDeployStatus_ReconciliationLifecycle(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("deploystatus@x.com", true)
	p := e.createProject("DeployStatus")

	createResp := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", GitHubRepo: "mon-site", GitHubToken: "ghp_secret123",
		Events: []string{"content.publish"},
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating webhook, got %d: %s", createResp.Code, createResp.Body.String())
	}
	wh := decodeBody[storage.Webhook](t, createResp)

	deliveredAt := time.Now().UTC()
	delivery := &storage.WebhookDelivery{
		WebhookID: wh.ID, ProjectID: p.ID, Event: "content.publish",
		Success: true, Attempts: 1, StatusCode: 204,
	}
	if err := e.server.System.RecordWebhookDelivery(bgCtx(), delivery); err != nil {
		t.Fatalf("record delivery: %v", err)
	}

	// Fake GitHub Actions API: starts by reporting the dispatch-triggered
	// run as in_progress, later flips to completed/success once told to.
	var runsListCalls, runCalls int32
	var runStatus atomic.Value
	runStatus.Store("in_progress")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/runs") && r.URL.Query().Get("event") == "repository_dispatch":
			atomic.AddInt32(&runsListCalls, 1)
			fmt.Fprintf(w, `{"workflow_runs":[{"id":777,"status":"in_progress","conclusion":"","html_url":"https://github.com/louis/mon-site/actions/runs/777","created_at":%q}]}`,
				deliveredAt.Add(time.Second).Format(time.RFC3339))
		case strings.HasSuffix(r.URL.Path, "/actions/runs/777"):
			atomic.AddInt32(&runCalls, 1)
			status := runStatus.Load().(string)
			conclusion := ""
			if status == "completed" {
				conclusion = "success"
			}
			fmt.Fprintf(w, `{"id":777,"status":%q,"conclusion":%q,"html_url":"https://github.com/louis/mon-site/actions/runs/777","created_at":%q}`,
				status, conclusion, deliveredAt.Add(time.Second).Format(time.RFC3339))
		default:
			t.Errorf("unexpected GitHub API call: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fake.Close()
	e.server.Dispatcher.GitHubAPIBaseURL = fake.URL

	// First poll: no run id known yet -> FindDispatchRun (the list endpoint).
	page1 := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if page1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", page1.Code, page1.Body.String())
	}
	if !strings.Contains(page1.Body.String(), "Déploiement en cours") || !strings.Contains(page1.Body.String(), "tri-badge-warning") {
		t.Fatalf("expected an in-progress deploy badge, got: %s", page1.Body.String())
	}
	if atomic.LoadInt32(&runsListCalls) != 1 {
		t.Fatalf("expected exactly 1 list-runs call to correlate the delivery, got %d", runsListCalls)
	}

	deliveries, err := e.server.System.ListWebhookDeliveries(bgCtx(), p.ID, 10)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("list deliveries: %v (%d)", err, len(deliveries))
	}
	if deliveries[0].DeployRunID != 777 || deliveries[0].DeployStatus != "in_progress" {
		t.Fatalf("expected correlated in_progress run persisted, got %+v", deliveries[0])
	}
	if deliveries[0].DeployFinal() {
		t.Fatal("in_progress must not be final")
	}

	// Second poll: a run id is now known -> GetRun (the specific-run
	// endpoint), not the list endpoint again.
	page2 := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if !strings.Contains(page2.Body.String(), "Déploiement en cours") {
		t.Fatalf("expected still in-progress on 2nd poll, got: %s", page2.Body.String())
	}
	if atomic.LoadInt32(&runsListCalls) != 1 {
		t.Fatalf("list-runs must not be called again once a run id is known, got %d calls", runsListCalls)
	}
	if atomic.LoadInt32(&runCalls) != 1 {
		t.Fatalf("expected exactly 1 get-run call on the 2nd poll, got %d", runCalls)
	}

	// The build finishes.
	runStatus.Store("completed")

	page3 := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if !strings.Contains(page3.Body.String(), "Déployé") || !strings.Contains(page3.Body.String(), "tri-badge-success") {
		t.Fatalf("expected a success deploy badge once completed, got: %s", page3.Body.String())
	}
	if !strings.Contains(page3.Body.String(), `href="https://github.com/louis/mon-site/actions/runs/777"`) {
		t.Fatalf("expected the badge to link to the run, got: %s", page3.Body.String())
	}
	deliveries, _ = e.server.System.ListWebhookDeliveries(bgCtx(), p.ID, 10)
	if !deliveries[0].DeployFinal() || deliveries[0].DeployConclusion != "success" {
		t.Fatalf("expected final success state persisted, got %+v", deliveries[0])
	}
	callsAfterCompletion := atomic.LoadInt32(&runCalls)

	// Final state: subsequent polls must NOT hit GitHub again -- this is
	// the whole point of persisting DeployStatus, not just displaying a
	// live value.
	page4 := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if !strings.Contains(page4.Body.String(), "Déployé") {
		t.Fatalf("expected success badge to persist across polls, got: %s", page4.Body.String())
	}
	if atomic.LoadInt32(&runCalls) != callsAfterCompletion {
		t.Fatalf("expected no further GitHub API calls once deploy state is final, calls went from %d to %d", callsAfterCompletion, runCalls)
	}
}

// TestHTMX_WebhookDeployStatus_DegradesWithoutActionsScope covers the
// migration scenario: an existing github_dispatch webhook's stored PAT
// only has the original Contents: Read and write scope (no Actions: Read),
// so the GitHub Actions API 403s. The page must still render normally --
// the delivery just shows "—" for deploy status -- not a server error.
func TestHTMX_WebhookDeployStatus_DegradesWithoutActionsScope(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("degrade@x.com", true)
	p := e.createProject("DegradeCheck")

	createResp := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", GitHubRepo: "mon-site", GitHubToken: "ghp_secret123",
		Events: []string{"content.publish"},
	})
	wh := decodeBody[storage.Webhook](t, createResp)
	if err := e.server.System.RecordWebhookDelivery(bgCtx(), &storage.WebhookDelivery{
		WebhookID: wh.ID, ProjectID: p.ID, Event: "content.publish", Success: true, Attempts: 1, StatusCode: 204,
	}); err != nil {
		t.Fatalf("record delivery: %v", err)
	}

	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
	}))
	defer forbidden.Close()
	e.server.Dispatcher.GitHubAPIBaseURL = forbidden.URL

	rec := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even when the GitHub Actions API is unreachable/forbidden, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<span class="tri-badge-outline">—</span>`) {
		t.Fatalf("expected the unresolved deploy status placeholder, got: %s", rec.Body.String())
	}
}
