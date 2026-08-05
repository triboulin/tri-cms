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

// Envelope is the JSON body POSTed to a webhook URL.
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

// Dispatcher sends webhook deliveries with retries and exponential backoff.
// Retries apply to network errors, timeouts, and 5xx/429 responses; any
// other 4xx response is treated as a permanent failure (no retry), per spec
// §5 requirement to cover both nominal and failure/retry paths explicitly.
type Dispatcher struct {
	Client      *http.Client
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration

	// Sleep and Now are overridable for deterministic, fast tests.
	Sleep func(time.Duration)
	Now   func() time.Time
}

// NewDispatcher returns a Dispatcher with sane production defaults.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		Client:      &http.Client{Timeout: 10 * time.Second},
		MaxAttempts: 5,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Sleep:       time.Sleep,
		Now:         time.Now,
	}
}

// Send delivers one event to one webhook, retrying transient failures.
func (d *Dispatcher) Send(ctx context.Context, wh *storage.Webhook, eventType string, data any) (*Result, error) {
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 1
	}
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

	res := &Result{}
	for attempt := 1; attempt <= d.MaxAttempts; attempt++ {
		res.Attempts = attempt

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("webhooks: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TriCMS-Event", eventType)
		req.Header.Set("X-TriCMS-Signature", "sha256="+signature)

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
