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

// TestDeployPoller_ResolvesLifecycle exercises the background poller
// directly (pollPendingDeploys), the write half of the deliberately
// decoupled polling design: it must correlate an uncorrelated delivery to
// its run, keep polling that run while non-final, and stop touching
// GitHub entirely once the run completes.
func TestDeployPoller_ResolvesLifecycle(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("poller@x.com", true)
	p := e.createProject("PollerLifecycle")

	createResp := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", GitHubRepo: "mon-site", GitHubToken: "ghp_secret",
		Events: []string{"content.publish"},
	})
	wh := decodeBody[storage.Webhook](t, createResp)

	deliveredAt := time.Now().UTC()
	if err := e.server.System.RecordWebhookDelivery(bgCtx(), &storage.WebhookDelivery{
		WebhookID: wh.ID, ProjectID: p.ID, Event: "content.publish", Success: true, Attempts: 1, StatusCode: 204,
	}); err != nil {
		t.Fatalf("record delivery: %v", err)
	}

	var runsListCalls, runCalls int32
	var status atomic.Value
	status.Store("in_progress")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/runs") && r.URL.Query().Get("event") == "repository_dispatch":
			atomic.AddInt32(&runsListCalls, 1)
			fmt.Fprintf(w, `{"workflow_runs":[{"id":42,"status":"in_progress","conclusion":"","html_url":"https://x/42","created_at":%q}]}`,
				deliveredAt.Add(time.Second).Format(time.RFC3339))
		case strings.HasSuffix(r.URL.Path, "/actions/runs/42"):
			atomic.AddInt32(&runCalls, 1)
			s := status.Load().(string)
			conclusion := ""
			if s == "completed" {
				conclusion = "success"
			}
			fmt.Fprintf(w, `{"id":42,"status":%q,"conclusion":%q,"html_url":"https://x/42","created_at":%q}`,
				s, conclusion, deliveredAt.Add(time.Second).Format(time.RFC3339))
		default:
			t.Errorf("unexpected GitHub API call: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fake.Close()
	e.server.Dispatcher.GitHubAPIBaseURL = fake.URL

	// Pass 1: not correlated yet -> FindDispatchRun (list endpoint).
	e.server.pollPendingDeploys(bgCtx())
	pending, err := e.server.System.ListPendingGitHubDispatchDeliveries(bgCtx(), time.Hour)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected the delivery still pending (in_progress): %v (%d)", err, len(pending))
	}
	if pending[0].DeployRunID != 42 || pending[0].DeployStatus != "in_progress" {
		t.Fatalf("expected correlated in_progress state, got %+v", pending[0])
	}
	if atomic.LoadInt32(&runsListCalls) != 1 {
		t.Fatalf("expected exactly 1 list-runs call, got %d", runsListCalls)
	}

	// Pass 2: run id known -> GetRun (specific-run endpoint), not the list again.
	e.server.pollPendingDeploys(bgCtx())
	if atomic.LoadInt32(&runsListCalls) != 1 {
		t.Fatalf("list-runs must not be called again once a run id is known, got %d", runsListCalls)
	}
	if atomic.LoadInt32(&runCalls) != 1 {
		t.Fatalf("expected exactly 1 get-run call on pass 2, got %d", runCalls)
	}

	// The build finishes.
	status.Store("completed")
	e.server.pollPendingDeploys(bgCtx())

	pending, err = e.server.System.ListPendingGitHubDispatchDeliveries(bgCtx(), time.Hour)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected the delivery to drop out of the pending set once resolved: %v (%d)", err, len(pending))
	}
	deliveries, _ := e.server.System.ListWebhookDeliveries(bgCtx(), p.ID, 10)
	if !deliveries[0].DeployFinal() || deliveries[0].DeployConclusion != "success" {
		t.Fatalf("expected final success state persisted, got %+v", deliveries[0])
	}
	callsAfterResolution := atomic.LoadInt32(&runCalls)

	// Further passes must not touch GitHub at all for this delivery.
	e.server.pollPendingDeploys(bgCtx())
	if atomic.LoadInt32(&runCalls) != callsAfterResolution {
		t.Fatalf("expected no further GitHub calls once resolved, went from %d to %d", callsAfterResolution, runCalls)
	}
}

// TestDeployPoller_DegradesOnAPIError covers a PAT that hasn't been
// granted the Actions: Read permission yet (or any other GitHub API
// failure): the poller must not panic or corrupt state, just leave the
// delivery pending for the next tick.
func TestDeployPoller_DegradesOnAPIError(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("pollerdegrade@x.com", true)
	p := e.createProject("PollerDegrade")

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

	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer forbidden.Close()
	e.server.Dispatcher.GitHubAPIBaseURL = forbidden.URL

	e.server.pollPendingDeploys(bgCtx())

	pending, err := e.server.System.ListPendingGitHubDispatchDeliveries(bgCtx(), time.Hour)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected the delivery to remain pending after a 403: %v (%d)", err, len(pending))
	}
	if pending[0].DeployStatus != "" {
		t.Fatalf("expected no deploy state written on API failure, got %+v", pending[0])
	}
}

// TestDeployPoller_RespectsMaxAge covers the age bound that stops the
// poller from retrying a delivery forever if its run can never be found.
func TestDeployPoller_RespectsMaxAge(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("pollerage@x.com", true)
	p := e.createProject("PollerAge")

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

	var calls int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, `{"workflow_runs":[]}`)
	}))
	defer fake.Close()
	e.server.Dispatcher.GitHubAPIBaseURL = fake.URL

	// A maxAge of 0 excludes every delivery, however recent.
	pending, err := e.server.System.ListPendingGitHubDispatchDeliveries(bgCtx(), 0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected nothing eligible with maxAge=0: %v (%d)", err, len(pending))
	}

	e.server.pollPendingDeploysWithMaxAge(bgCtx(), 0)
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("expected no GitHub calls for a delivery outside the age window, got %d", calls)
	}
}
