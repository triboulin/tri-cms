package webhooks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
		if r.Header.Get("X-TriCMS-Event") != EventContentCreate {
			t.Errorf("expected event header, got %q", r.Header.Get("X-TriCMS-Event"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := testDispatcher()
	wh := &storage.Webhook{URL: srv.URL, Secret: "s3cret", ProjectID: "proj_1"}
	res, err := d.Send(context.Background(), wh, EventContentCreate, map[string]string{"id": "c1"})
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
	res, err := d.Send(context.Background(), wh, EventContentDelete, nil)
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
	res, err := d.Send(context.Background(), wh, EventContentCreate, nil)
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
	res, err := d.Send(context.Background(), wh, EventContentDelete, nil)
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
	if _, err := d.Send(context.Background(), wh, EventContentCreate, nil); err == nil {
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
	res, err := d.Send(context.Background(), wh, EventContentCreate, nil)
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
}

var _ = errors.New
