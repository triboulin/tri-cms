package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"tricms/pkg/storage"
)

// Envelope is the JSON body POSTed to a webhook URL for kind=generic
// (KindGeneric) deliveries.
type Envelope struct {
	Event     string `json:"event"`
	ProjectID string `json:"project_id"`
	Data      any    `json:"data"`
	Timestamp string `json:"timestamp"`
}

// Result summarizes a dispatch attempt sequence for one webhook delivery.
type Result struct {
	Success        bool
	Attempts       int
	LastStatusCode int
	LastError      string
}

// Webhook delivery kinds. KindGeneric (the original, and the default for
// any webhook that doesn't set Kind) POSTs a signed Envelope to an
// arbitrary URL. Other kinds speak a fixed third-party API contract
// instead, so they don't go through the generic signed-envelope path at
// all -- see GitHubDispatchConfig.
const (
	KindGeneric        = "generic"
	KindGitHubDispatch = "github_dispatch"
)

// GitHubDispatchConfig is the storage.Webhook.Config JSON shape for
// KindGitHubDispatch webhooks: it triggers a GitHub Actions workflow via
// the repos/{owner}/{repo}/dispatches API instead of delivering to an
// arbitrary URL. Token is a plaintext PAT only transiently (API request
// bodies, or right after Decryptor.Decrypt); at rest in storage.Webhook.Config
// it is AES-GCM ciphertext produced by pkg/auth.Encryptor.
type GitHubDispatchConfig struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Token string `json:"token"`
}

// Decryptor decrypts secrets embedded in a webhook's Config (currently just
// GitHubDispatchConfig.Token). It's an interface -- rather than a direct
// dependency on pkg/auth.Encryptor -- so tests can stub it without needing
// a real key, and so pkg/webhooks doesn't have to know how encryption is
// implemented.
type Decryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// Dispatcher sends webhook deliveries with retries and exponential backoff.
// Retries apply to network errors, timeouts, and 5xx/429 responses; any
// other 4xx response is treated as a permanent failure (no retry), per spec
// §5 requirement to cover both nominal and failure/retry paths explicitly.
// This retry policy is shared by every Kind (see deliver).
type Dispatcher struct {
	Client      *http.Client
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration

	// GitHubAPIBaseURL is the base URL for KindGitHubDispatch deliveries.
	// Defaults to the real GitHub API; overridable in tests.
	GitHubAPIBaseURL string

	// Decryptor decrypts KindGitHubDispatch tokens before use. Required
	// only for that kind; NewDispatcher leaves it nil, so wire it in
	// (cmd/tricms/main.go) once an encryption key is available.
	Decryptor Decryptor

	// Sleep and Now are overridable for deterministic, fast tests.
	Sleep func(time.Duration)
	Now   func() time.Time
}

// NewDispatcher returns a Dispatcher with sane production defaults.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		Client:           &http.Client{Timeout: 10 * time.Second},
		MaxAttempts:      5,
		BaseDelay:        500 * time.Millisecond,
		MaxDelay:         30 * time.Second,
		GitHubAPIBaseURL: "https://api.github.com",
		Sleep:            time.Sleep,
		Now:              time.Now,
	}
}

// Send delivers one event to one webhook, retrying transient failures.
// The delivery mechanism depends on wh.Kind: KindGeneric (default, empty
// string included) sends a signed Envelope to wh.URL; KindGitHubDispatch
// ignores wh.URL/wh.Secret entirely and instead triggers a GitHub Actions
// workflow via wh.Config (see GitHubDispatchConfig).
func (d *Dispatcher) Send(ctx context.Context, wh *storage.Webhook, eventType string, data any) (*Result, error) {
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 1
	}
	if wh.Kind == KindGitHubDispatch {
		return d.sendGitHubDispatch(ctx, wh, eventType)
	}
	return d.sendGeneric(ctx, wh, eventType, data)
}

func (d *Dispatcher) sendGeneric(ctx context.Context, wh *storage.Webhook, eventType string, data any) (*Result, error) {
	env := Envelope{
		Event:     eventType,
		ProjectID: wh.ProjectID,
		Data:      data,
		Timestamp: d.now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("webhooks: marshal envelope: %w", err)
	}
	signature := sign(wh.Secret, body)

	return d.deliver(ctx, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TriCMS-Event", eventType)
		req.Header.Set("X-TriCMS-Signature", "sha256="+signature)
		return req, nil
	})
}

// sendGitHubDispatch triggers a `repository_dispatch` event on the GitHub
// repo configured in wh.Config, so a site's CI can rebuild+redeploy when
// content is published in triCMS, without an intermediate relay: triCMS
// already makes outbound HTTPS calls for generic webhooks (see Dockerfile,
// CA certs shipped for exactly this), so calling api.github.com directly is
// no new capability, just a different request shape.
func (d *Dispatcher) sendGitHubDispatch(ctx context.Context, wh *storage.Webhook, eventType string) (*Result, error) {
	var cfg GitHubDispatchConfig
	if err := json.Unmarshal([]byte(wh.Config), &cfg); err != nil {
		return nil, fmt.Errorf("webhooks: parse github_dispatch config: %w", err)
	}
	if cfg.Owner == "" || cfg.Repo == "" || cfg.Token == "" {
		return nil, fmt.Errorf("webhooks: github_dispatch config missing owner/repo/token")
	}
	if d.Decryptor == nil {
		return nil, fmt.Errorf("webhooks: no decryptor configured for github_dispatch webhooks")
	}
	token, err := d.Decryptor.Decrypt(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("webhooks: decrypt github token: %w", err)
	}

	body, err := json.Marshal(map[string]string{"event_type": eventType})
	if err != nil {
		return nil, fmt.Errorf("webhooks: marshal dispatch body: %w", err)
	}
	base := d.GitHubAPIBaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/%s/dispatches", base, cfg.Owner, cfg.Repo)

	return d.deliver(ctx, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "tricms-webhooks")
		return req, nil
	})
}

// deliver runs the retry/backoff loop shared by every Kind, building a
// fresh request each attempt via buildRequest (bodies are precomputed by
// the caller and reused; only the *http.Request wrapper is rebuilt, since
// http.Request must not be reused across attempts).
func (d *Dispatcher) deliver(ctx context.Context, buildRequest func(context.Context) (*http.Request, error)) (*Result, error) {
	res := &Result{}
	for attempt := 1; attempt <= d.MaxAttempts; attempt++ {
		res.Attempts = attempt

		req, err := buildRequest(ctx)
		if err != nil {
			return nil, fmt.Errorf("webhooks: build request: %w", err)
		}

		resp, err := d.Client.Do(req)
		if err != nil {
			res.LastError = err.Error()
			if attempt == d.MaxAttempts {
				return res, nil
			}
			d.wait(attempt)
			continue
		}

		func() {
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()
		res.LastStatusCode = resp.StatusCode

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			res.Success = true
			res.LastError = ""
			return res, nil
		}

		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		res.LastError = fmt.Sprintf("unexpected status code %d", resp.StatusCode)
		if !retryable {
			return res, nil // permanent 4xx failure: stop, do not retry
		}
		if attempt == d.MaxAttempts {
			return res, nil
		}
		d.wait(attempt)
	}
	return res, nil
}

func (d *Dispatcher) wait(attempt int) {
	delay := d.BaseDelay << (attempt - 1) // exponential backoff
	if delay > d.MaxDelay {
		delay = d.MaxDelay
	}
	d.Sleep(delay)
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// sign computes the hex-encoded HMAC-SHA256 of body using secret, allowing
// receivers to verify payload authenticity/integrity.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature is a helper receivers (or our own tests) can use to check
// an incoming `X-TriCMS-Signature` header against the expected secret.
func VerifySignature(secret string, body []byte, signatureHeader string) bool {
	const prefix = "sha256="
	if len(signatureHeader) <= len(prefix) || signatureHeader[:len(prefix)] != prefix {
		return false
	}
	expected := sign(secret, body)
	given := signatureHeader[len(prefix):]
	return hmac.Equal([]byte(expected), []byte(given))
}
