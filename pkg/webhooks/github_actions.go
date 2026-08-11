package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RunInfo summarizes a GitHub Actions workflow run's current state, as
// reported by the Actions API (see FindDispatchRun/GetRun below).
type RunInfo struct {
	ID         int64
	Status     string // "queued" | "in_progress" | "completed"
	Conclusion string // "" until Status == "completed", then "success" | "failure" | "cancelled" | "timed_out" | "action_required" | "stale" | "skipped" | "neutral"
	HTMLURL    string
}

// dispatchRunSkew absorbs clock skew between this server and GitHub's when
// correlating a run to "the dispatch we just sent" by timestamp -- a run
// created a couple seconds before our recorded dispatch time is still very
// likely ours, not a coincidence.
const dispatchRunSkew = 10 * time.Second

type ghRun struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	CreatedAt  string `json:"created_at"`
}

func (r ghRun) toRunInfo() *RunInfo {
	return &RunInfo{ID: r.ID, Status: r.Status, Conclusion: r.Conclusion, HTMLURL: r.HTMLURL}
}

type ghRunsListResponse struct {
	WorkflowRuns []ghRun `json:"workflow_runs"`
}

// FindDispatchRun looks for the workflow run a repository_dispatch call
// most likely triggered. GitHub's dispatches endpoint (see
// Dispatcher.sendGitHubDispatch) returns no run id synchronously -- a
// repository_dispatch event just gets queued for whichever workflow(s)
// listen for it -- so this instead lists the repo's most recent
// repository_dispatch-triggered runs and picks the oldest one still at or
// after `after` (our own recorded dispatch time, minus dispatchRunSkew for
// clock skew). Reliable as long as this repo isn't triggering concurrent
// repository_dispatch runs from multiple sources in the same few seconds --
// true for the single-webhook-per-project setup this is built for.
//
// Returns (nil, nil), not an error, when no matching run is found yet --
// completely normal in the few seconds between sending the dispatch and
// GitHub actually queuing the run; the caller should just try again on the
// next poll.
func (d *Dispatcher) FindDispatchRun(ctx context.Context, owner, repo, encryptedToken string, after time.Time) (*RunInfo, error) {
	var list ghRunsListResponse
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?event=repository_dispatch&per_page=10", d.githubAPIBase(), owner, repo)
	if err := d.githubGET(ctx, url, encryptedToken, &list); err != nil {
		return nil, err
	}

	// Runs come back newest-first; walk them keeping the oldest one that's
	// still within our window, which is the run closest to (and at/after)
	// our own dispatch call.
	var found *ghRun
	cutoff := after.Add(-dispatchRunSkew)
	for i := range list.WorkflowRuns {
		r := &list.WorkflowRuns[i]
		createdAt, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			continue
		}
		if createdAt.Before(cutoff) {
			break
		}
		found = r
	}
	if found == nil {
		return nil, nil
	}
	return found.toRunInfo(), nil
}

// GetRun fetches the current status of a specific, already-identified run
// (the one FindDispatchRun previously correlated), for polling it to
// completion.
func (d *Dispatcher) GetRun(ctx context.Context, owner, repo, encryptedToken string, runID int64) (*RunInfo, error) {
	var r ghRun
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", d.githubAPIBase(), owner, repo, runID)
	if err := d.githubGET(ctx, url, encryptedToken, &r); err != nil {
		return nil, err
	}
	return r.toRunInfo(), nil
}

func (d *Dispatcher) githubAPIBase() string {
	if d.GitHubAPIBaseURL == "" {
		return "https://api.github.com"
	}
	return d.GitHubAPIBaseURL
}

// githubGET decrypts encryptedToken, issues an authenticated GET against
// the GitHub REST API, and decodes a 200 JSON response into out. Any
// non-200 response (commonly 401/403 when the stored PAT lacks the
// Actions: Read permission dispatch-only tokens weren't originally granted)
// or transport error comes back as a plain error -- callers of
// FindDispatchRun/GetRun are expected to treat that as "couldn't determine
// deploy status right now" and degrade silently, not surface it as an
// application error.
func (d *Dispatcher) githubGET(ctx context.Context, url, encryptedToken string, out any) error {
	if d.Decryptor == nil {
		return fmt.Errorf("webhooks: no decryptor configured for github actions api")
	}
	token, err := d.Decryptor.Decrypt(encryptedToken)
	if err != nil {
		return fmt.Errorf("webhooks: decrypt github token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "tricms-webhooks")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("webhooks: github actions api %s: %d %s", url, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
