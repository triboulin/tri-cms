package webhooks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tricms/pkg/storage"
)

func testDispatcher() *Dispatcher {
	var sleeps []time.Duration
	d := NewDispatcher()
	d.MaxAttempts = 4
	d.BaseDelay = time.Millisecond
	d.MaxDelay = 10 * time.Millisecond
	d.Sleep = func(dur time.Duration) { sleeps = append(sleeps, dur) }
	_ = sleeps
	return d
}

func TestDispatcher_SuccessFirstAttempt(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get("X-TriCMS-Signature")
		if !VerifySignature("s3cret", body, sig) {
			t.Errorf("signature verification failed")
		}
		if r.Header.Get("X-TriCMS-Event") != EventContentUpdate {
			t.Errorf("expected event header, got %q", r.Header.Get("X-TriCMS-Event"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := testDispatcher()
	wh := &storage.Webhook{URL: srv.URL, Secret: "s3cret", ProjectID: "proj_1"}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, map[string]string{"id": "c1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success || res.Attempts != 1 {
		t.Fatalf("expected success on first attempt, got %+v", res)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 HTTP call, got %d", calls)
	}
}

func TestDispatcher_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := testDispatcher()
	wh := &storage.Webhook{URL: srv.URL, Secret: "s", ProjectID: "p"}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success || res.Attempts != 3 {
		t.Fatalf("expected success on 3rd attempt, got %+v", res)
	}
}

func TestDispatcher_PermanentFailureOn4xxNoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	d := testDispatcher()
	wh := &storage.Webhook{URL: srv.URL, Secret: "s", ProjectID: "p"}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success || res.Attempts != 1 {
		t.Fatalf("expected permanent failure without retry, got %+v", res)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 HTTP call (no retry on 4xx), got %d", calls)
	}
}

func TestDispatcher_ExhaustsRetriesOnPersistent5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := testDispatcher()
	wh := &storage.Webhook{URL: srv.URL, Secret: "s", ProjectID: "p"}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success || res.Attempts != d.MaxAttempts {
		t.Fatalf("expected failure after exhausting %d attempts, got %+v", d.MaxAttempts, res)
	}
	if int(atomic.LoadInt32(&calls)) != d.MaxAttempts {
		t.Fatalf("expected %d calls, got %d", d.MaxAttempts, calls)
	}
}

func TestDispatcher_RetriesOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	d := testDispatcher()
	wh := &storage.Webhook{URL: srv.URL, Secret: "s", ProjectID: "p"}
	res, err := d.Send(context.Background(), wh, EventMediaCreate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success || res.Attempts != 2 {
		t.Fatalf("expected success on 2nd attempt after 429, got %+v", res)
	}
}

func TestDispatcher_NetworkErrorRetriesThenGivesUp(t *testing.T) {
	d := testDispatcher()
	d.MaxAttempts = 2
	// Point at a closed connection to force a network error on every attempt.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // server is now unreachable

	wh := &storage.Webhook{URL: url, Secret: "s", ProjectID: "p"}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success || res.Attempts != 2 || res.LastError == "" {
		t.Fatalf("expected exhausted network-error retries, got %+v", res)
	}
}

func TestDispatcher_ContextCancelledBuildingRequestFails(t *testing.T) {
	d := testDispatcher()
	wh := &storage.Webhook{URL: "://bad-url", Secret: "s", ProjectID: "p"}
	if _, err := d.Send(context.Background(), wh, EventContentUpdate, nil); err == nil {
		t.Fatal("expected error building request for malformed URL")
	}
}

func TestDispatcher_DefaultAttemptsWhenZero(t *testing.T) {
	d := testDispatcher()
	d.MaxAttempts = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	wh := &storage.Webhook{URL: srv.URL, Secret: "s", ProjectID: "p"}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, nil)
	if err != nil || !res.Success || res.Attempts != 1 {
		t.Fatalf("expected 1 attempt with default, got %+v (err=%v)", res, err)
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"a":1}`)
	sig := sign("secret", body)
	if !VerifySignature("secret", body, "sha256="+sig) {
		t.Fatal("expected signature to verify")
	}
	if VerifySignature("wrong-secret", body, "sha256="+sig) {
		t.Fatal("expected signature verification to fail with wrong secret")
	}
	if VerifySignature("secret", body, "not-prefixed") {
		t.Fatal("expected missing sha256= prefix to fail")
	}
	if VerifySignature("secret", body, "sha256=") {
		t.Fatal("expected empty signature to fail")
	}
}

func TestNewDispatcher_Defaults(t *testing.T) {
	d := NewDispatcher()
	if d.MaxAttempts != 5 || d.Client == nil || d.Sleep == nil || d.Now == nil {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if d.GitHubAPIBaseURL != "https://api.github.com" {
		t.Fatalf("unexpected default GitHubAPIBaseURL: %q", d.GitHubAPIBaseURL)
	}
}

// stubDecryptor is a fake Decryptor for tests: it just strips a fixed
// prefix, so tests don't need a real pkg/auth.Encryptor to exercise the
// github_dispatch delivery path.
type stubDecryptor struct {
	prefix string
	err    error
}

func (s stubDecryptor) Decrypt(ciphertext string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return strings.TrimPrefix(ciphertext, s.prefix), nil
}

func TestDispatcher_GitHubDispatch_Success(t *testing.T) {
	var gotAuth, gotAccept, gotBody string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent) // GitHub returns 204 on success
	}))
	defer srv.Close()

	d := testDispatcher()
	d.GitHubAPIBaseURL = srv.URL
	d.Decryptor = stubDecryptor{prefix: "enc:"}

	wh := &storage.Webhook{
		Kind:   KindGitHubDispatch,
		Config: `{"owner":"louis","repo":"site","token":"enc:ghp_abc123"}`,
	}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success || res.Attempts != 1 {
		t.Fatalf("expected success on first attempt, got %+v", res)
	}
	if gotPath != "/repos/louis/site/dispatches" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotAuth != "Bearer ghp_abc123" {
		t.Fatalf("expected decrypted token in Authorization header, got %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Fatalf("unexpected Accept header: %q", gotAccept)
	}
	if gotBody != `{"event_type":"content.update"}` {
		t.Fatalf("unexpected dispatch body: %q", gotBody)
	}
}

func TestDispatcher_GitHubDispatch_RetriesOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := testDispatcher()
	d.GitHubAPIBaseURL = srv.URL
	d.Decryptor = stubDecryptor{}

	wh := &storage.Webhook{Kind: KindGitHubDispatch, Config: `{"owner":"o","repo":"r","token":"t"}`}
	res, err := d.Send(context.Background(), wh, EventContentUpdate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success || res.Attempts != 2 {
		t.Fatalf("expected success on 2nd attempt, got %+v", res)
	}
}

func TestDispatcher_GitHubDispatch_MissingDecryptor(t *testing.T) {
	d := testDispatcher()
	wh := &storage.Webhook{Kind: KindGitHubDispatch, Config: `{"owner":"o","repo":"r","token":"t"}`}
	if _, err := d.Send(context.Background(), wh, EventContentUpdate, nil); err == nil {
		t.Fatal("expected error when no Decryptor is configured")
	}
}

func TestDispatcher_GitHubDispatch_DecryptFailure(t *testing.T) {
	d := testDispatcher()
	d.Decryptor = stubDecryptor{err: errors.New("bad key")}
	wh := &storage.Webhook{Kind: KindGitHubDispatch, Config: `{"owner":"o","repo":"r","token":"t"}`}
	if _, err := d.Send(context.Background(), wh, EventContentUpdate, nil); err == nil {
		t.Fatal("expected error when decryption fails")
	}
}

func TestDispatcher_GitHubDispatch_MalformedConfig(t *testing.T) {
	d := testDispatcher()
	d.Decryptor = stubDecryptor{}
	wh := &storage.Webhook{Kind: KindGitHubDispatch, Config: `not-json`}
	if _, err := d.Send(context.Background(), wh, EventContentUpdate, nil); err == nil {
		t.Fatal("expected error for malformed config JSON")
	}
}

func TestDispatcher_GitHubDispatch_MissingFields(t *testing.T) {
	d := testDispatcher()
	d.Decryptor = stubDecryptor{}
	cases := []string{
		`{"repo":"r","token":"t"}`,
		`{"owner":"o","token":"t"}`,
		`{"owner":"o","repo":"r"}`,
	}
	for _, cfg := range cases {
		wh := &storage.Webhook{Kind: KindGitHubDispatch, Config: cfg}
		if _, err := d.Send(context.Background(), wh, EventContentUpdate, nil); err == nil {
			t.Errorf("config %q: expected error for missing required field", cfg)
		}
	}
}

func TestDispatcher_GitHubDispatch_DefaultsBaseURL(t *testing.T) {
	d := NewDispatcher()
	d.Decryptor = stubDecryptor{}
	d.GitHubAPIBaseURL = "" // force the fallback branch in sendGitHubDispatch
	d.MaxAttempts = 1
	d.Client = &http.Client{Timeout: time.Millisecond} // fail fast, we only care the URL was built
	wh := &storage.Webhook{Kind: KindGitHubDispatch, Config: `{"owner":"o","repo":"r","token":"t"}`}
	// Real api.github.com may or may not be reachable in this sandbox; we
	// only assert this doesn't panic/error on request *construction*.
	if _, err := d.Send(context.Background(), wh, EventContentUpdate, nil); err != nil {
		t.Fatalf("unexpected request-construction error: %v", err)
	}
}

var _ = errors.New
