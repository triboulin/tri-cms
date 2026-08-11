package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDispatcher_FindDispatchRun_Success(t *testing.T) {
	var gotPath, gotAuth string
	now := time.Now().UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprintf(w, `{"workflow_runs":[
			{"id":333,"status":"queued","conclusion":"","html_url":"https://x/333","created_at":%q},
			{"id":222,"status":"completed","conclusion":"success","html_url":"https://x/222","created_at":%q},
			{"id":111,"status":"completed","conclusion":"failure","html_url":"https://x/111","created_at":%q}
		]}`,
			now.Add(2*time.Second).Format(time.RFC3339),
			now.Add(1*time.Second).Format(time.RFC3339),
			now.Add(-time.Hour).Format(time.RFC3339),
		)
	}))
	defer srv.Close()

	d := testDispatcher()
	d.GitHubAPIBaseURL = srv.URL
	d.Decryptor = stubDecryptor{prefix: "enc:"}

	run, err := d.FindDispatchRun(context.Background(), "louis", "site", "enc:ghp_abc", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run == nil {
		t.Fatal("expected a matched run, got nil")
	}
	// Run 111 predates `now` by an hour (outside the skew window) so it must
	// be excluded; between 222 and 333, the oldest one still >= now (222,
	// created 1s after) is the closest match to our own dispatch call.
	if run.ID != 222 {
		t.Fatalf("expected run 222 (closest to dispatch time), got %d", run.ID)
	}
	if gotPath != "/repos/louis/site/actions/runs?event=repository_dispatch&per_page=10" {
		t.Fatalf("unexpected request path: %q", gotPath)
	}
	if gotAuth != "Bearer ghp_abc" {
		t.Fatalf("expected decrypted token in Authorization header, got %q", gotAuth)
	}
}

func TestDispatcher_FindDispatchRun_NoMatchYet(t *testing.T) {
	now := time.Now().UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only an old, unrelated run exists -- our dispatch hasn't been
		// picked up by GitHub yet.
		fmt.Fprintf(w, `{"workflow_runs":[{"id":1,"status":"completed","conclusion":"success","html_url":"https://x/1","created_at":%q}]}`,
			now.Add(-time.Hour).Format(time.RFC3339))
	}))
	defer srv.Close()

	d := testDispatcher()
	d.GitHubAPIBaseURL = srv.URL
	d.Decryptor = stubDecryptor{}

	run, err := d.FindDispatchRun(context.Background(), "o", "r", "t", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Fatalf("expected no match, got %+v", run)
	}
}

func TestDispatcher_FindDispatchRun_ToleratesClockSkew(t *testing.T) {
	now := time.Now().UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A run created 5s before `now` (well within dispatchRunSkew) should
		// still match -- this server's clock can run slightly ahead.
		fmt.Fprintf(w, `{"workflow_runs":[{"id":42,"status":"in_progress","conclusion":"","html_url":"https://x/42","created_at":%q}]}`,
			now.Add(-5*time.Second).Format(time.RFC3339))
	}))
	defer srv.Close()

	d := testDispatcher()
	d.GitHubAPIBaseURL = srv.URL
	d.Decryptor = stubDecryptor{}

	run, err := d.FindDispatchRun(context.Background(), "o", "r", "t", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run == nil || run.ID != 42 {
		t.Fatalf("expected run 42 to match within skew tolerance, got %+v", run)
	}
}

func TestDispatcher_GetRun(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"id":42,"status":"completed","conclusion":"failure","html_url":"https://x/42","created_at":"2026-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	d := testDispatcher()
	d.GitHubAPIBaseURL = srv.URL
	d.Decryptor = stubDecryptor{}

	run, err := d.GetRun(context.Background(), "o", "r", "t", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != "completed" || run.Conclusion != "failure" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if gotPath != "/repos/o/r/actions/runs/42" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
}

func TestDispatcher_GitHubActionsAPI_DegradesOnErrors(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden) // e.g. PAT missing the Actions: Read scope
		fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
	}))
	defer forbidden.Close()

	d := testDispatcher()
	d.GitHubAPIBaseURL = forbidden.URL
	d.Decryptor = stubDecryptor{}

	if _, err := d.FindDispatchRun(context.Background(), "o", "r", "t", time.Now()); err == nil {
		t.Fatal("expected an error on 403, so the caller can skip this delivery this round")
	}
	if _, err := d.GetRun(context.Background(), "o", "r", "t", 1); err == nil {
		t.Fatal("expected an error on 403 for GetRun too")
	}

	// Missing decryptor / decrypt failure must also error out cleanly, same
	// as the dispatch path already does.
	d2 := testDispatcher()
	if _, err := d2.FindDispatchRun(context.Background(), "o", "r", "t", time.Now()); err == nil {
		t.Fatal("expected error with no Decryptor configured")
	}
	d3 := testDispatcher()
	d3.Decryptor = stubDecryptor{err: errors.New("bad key")}
	if _, err := d3.GetRun(context.Background(), "o", "r", "t", 1); err == nil {
		t.Fatal("expected error when decryption fails")
	}
}
