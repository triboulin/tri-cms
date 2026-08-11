package api

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

// DeployStatusPollInterval is how often the background poller checks
// pending github_dispatch deliveries for their downstream run's status.
const DeployStatusPollInterval = 5 * time.Second

// DeployStatusMaxAge bounds how long a delivery stays eligible for
// polling -- see storage.ListPendingGitHubDispatchDeliveries. Long enough
// for any realistic build, short enough that a delivery whose run can
// never be found (mistyped owner/repo, a workflow that doesn't actually
// listen for the dispatch) doesn't get retried forever.
const DeployStatusMaxAge = 30 * time.Minute

// RunDeployStatusPoller runs forever (until ctx is cancelled), periodically
// reconciling every project's pending github_dispatch deliveries against
// the GitHub Actions API and persisting whatever it learns.
//
// This is deliberately the *only* place that talks to the GitHub Actions
// API for deploy status -- a two-sided design with the database as the
// sole hand-off: this loop writes, independently of whether anyone has a
// browser open; the Webhooks page (htmxWebhooks) and the topbar deploy
// tile (htmxDeployTile) only ever read. Neither side calls the other
// directly, so a slow/unreachable GitHub API can stall this loop's own
// next tick but never a page load, and the tile/history stay meaningful
// even if this loop has been down for a while (they just show the last
// known state).
//
// cmd/tricms/main.go launches this as a goroutine right after building the
// Server. A no-op tick (nothing pending) is one indexed SQL query, so
// there's no need to gate it behind "does any project use github_dispatch
// webhooks" -- simpler to always run it.
func (s *Server) RunDeployStatusPoller(ctx context.Context) {
	if s.Dispatcher == nil {
		return
	}
	ticker := time.NewTicker(DeployStatusPollInterval)
	defer ticker.Stop()
	for {
		s.pollPendingDeploys(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// pollPendingDeploys runs one reconciliation pass across every project,
// using the production age bound (DeployStatusMaxAge).
func (s *Server) pollPendingDeploys(ctx context.Context) {
	s.pollPendingDeploysWithMaxAge(ctx, DeployStatusMaxAge)
}

// pollPendingDeploysWithMaxAge is pollPendingDeploys with an overridable
// age bound, split out so tests can exercise the bound itself (see
// deploy_poller_test.go) without waiting DeployStatusMaxAge for real.
func (s *Server) pollPendingDeploysWithMaxAge(ctx context.Context, maxAge time.Duration) {
	pending, err := s.System.ListPendingGitHubDispatchDeliveries(ctx, maxAge)
	if err != nil {
		log.Printf("deploy status poller: list pending deliveries: %v", err)
		return
	}
	for _, d := range pending {
		s.pollOneDelivery(ctx, d)
	}
}

// pollOneDelivery correlates (or, once correlated, re-polls) a single
// delivery's downstream run and persists whatever it learns. Any failure
// -- webhook deleted since, malformed config, GitHub API error including a
// 401/403 from a PAT missing the Actions: Read permission -- is logged and
// otherwise swallowed: this delivery is simply tried again next tick,
// never treated as fatal to the poll loop.
func (s *Server) pollOneDelivery(ctx context.Context, d *storage.WebhookDelivery) {
	wh, err := s.System.GetWebhook(ctx, d.WebhookID)
	if err != nil {
		return // webhook deleted since this delivery was recorded
	}
	var cfg webhooks.GitHubDispatchConfig
	if err := json.Unmarshal([]byte(wh.Config), &cfg); err != nil {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var run *webhooks.RunInfo
	if d.DeployRunID != 0 {
		run, err = s.Dispatcher.GetRun(reqCtx, cfg.Owner, cfg.Repo, cfg.Token, d.DeployRunID)
	} else {
		run, err = s.Dispatcher.FindDispatchRun(reqCtx, cfg.Owner, cfg.Repo, cfg.Token, d.CreatedAt)
	}
	if err != nil || run == nil {
		return // couldn't tell right now, or GitHub hasn't queued the run yet -- retry next tick
	}
	if err := s.System.UpdateWebhookDeliveryDeployState(ctx, d.ID, run.ID, run.Status, run.Conclusion, run.HTMLURL); err != nil {
		log.Printf("deploy status poller: persist state for delivery %d: %v", d.ID, err)
	}
}
