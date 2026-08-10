package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

// TestAPITokens_AdminOnlyAndTokenIsGlobalAdmin covers the current design:
// tokens live in the global Administration scope (not under any project),
// only a global ADMIN may mint or list them, and every minted token is
// ADMIN-equivalent -- it works against *any* project, not just one it was
// "created for" (there's no such notion anymore; see pkg/api/router.go and
// pkg/api/middleware.go).
func TestAPITokens_AdminOnlyAndTokenIsGlobalAdmin(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("t1@x.com", true)
	concepteur := e.createUser("t2@x.com", false)
	p := e.createProject("API Project")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	// Non-admin cannot create tokens.
	rec := e.request(http.MethodPost, "/api/v1/tokens", concepteur, createTokenRequest{Name: "CI"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	rec = e.request(http.MethodPost, "/api/v1/tokens", admin, createTokenRequest{Name: "CI"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[createTokenResponse](t, rec)
	if created.Token == "" {
		t.Fatal("expected plaintext token to be returned once")
	}

	// Non-admin cannot list tokens either.
	rec = e.request(http.MethodGet, "/api/v1/tokens", concepteur, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 listing tokens as non-admin, got %d", rec.Code)
	}

	rec = e.request(http.MethodGet, "/api/v1/tokens", admin, nil)
	list := decodeBody[[]tokenView](t, rec)
	if len(list) != 1 {
		t.Fatalf("expected 1 token, got %d", len(list))
	}

	// Set up a schema so the token can be used against content endpoints.
	setupSchema(t, e, p.ID, concepteur, "item", trischema.Definition{Fields: []trischema.Field{
		{Key: "name", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
	}})

	// Bearer token grants project content access without any session.
	rec = e.requestWithToken(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/item", created.Token, contentRequest{
		Data: map[string]any{"name": "Widget"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 via API token, got %d: %s", rec.Code, rec.Body.String())
	}

	// The same token also works against a *different* project: it's global
	// ADMIN-equivalent, not scoped to whichever project it was minted near.
	other := e.createProject("Other")
	rec = e.requestWithToken(http.MethodGet, "/api/v1/projects/"+other.ID+"/contents/item", created.Token, nil)
	if rec.Code != http.StatusOK {
		// "item" schema doesn't exist in `other`, so the list is simply empty
		// -- crucially not 403: the token itself is authorized for this project.
		t.Fatalf("expected 200 (empty list, not a permission error) for a different project, got %d: %s", rec.Code, rec.Body.String())
	}

	// A token can also list every project (ADMIN-equivalent), not just ones
	// it happens to have interacted with.
	rec = e.requestWithToken(http.MethodGet, "/api/v1/projects", created.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 listing all projects via token, got %d", rec.Code)
	}
	projects := decodeBody[[]storage.Project](t, rec)
	if len(projects) != 2 {
		t.Fatalf("expected token to see all %d projects, got %d", 2, len(projects))
	}

	// Revoke.
	rec = e.request(http.MethodDelete, "/api/v1/tokens/"+created.ID, admin, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	rec = e.requestWithToken(http.MethodGet, "/api/v1/projects/"+p.ID+"/contents/item", created.Token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after token revocation (no longer resolves to any identity), got %d", rec.Code)
	}
}

func TestWebhooks_AdminOnlyCRUD(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("w1@x.com", true)
	concepteur := e.createUser("w2@x.com", false)
	p := e.createProject("Hooks")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", concepteur, webhookRequest{URL: "https://example.com", Secret: "s", Events: []string{"content.create"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for CONCEPTEUR (webhooks are admin-only), got %d", rec.Code)
	}

	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{URL: "https://example.com", Secret: "s", Events: []string{"content.create"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	wh := decodeBody[storage.Webhook](t, rec)

	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/webhooks/"+wh.ID, admin, webhookRequest{URL: "https://example.com/v2", Secret: "s2", Events: []string{"content.delete"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/webhooks", admin, nil)
	list := decodeBody[[]storage.Webhook](t, rec)
	if len(list) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(list))
	}

	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/webhooks/"+wh.ID, admin, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

// TestWebhooks_HTMXNewFormChecksAllEventsByDefault guards the HTMX
// create-webhook form specifically (not just the JSON API's default in
// buildWebhookFields): the "Nouveau webhook" checkbox list must render
// every event pre-checked, since a webhook submitted as-is is meant to
// subscribe to everything. A prior version of the template rendered the
// options without ever emitting the `checked` attribute at all, even
// though the handler passed Selected:true for each -- silently defeating
// the default despite the underlying data being correct.
func TestWebhooks_HTMXNewFormChecksAllEventsByDefault(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("wform1@x.com", true)
	p := e.createProject("FormDefaults")

	rec := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for webhooks page, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, event := range availableWebhookEvents() {
		needle := `value="` + event + `" checked`
		if !strings.Contains(body, needle) {
			t.Fatalf("expected new-webhook form to pre-check %q (looking for %q), got: %s", event, needle, body)
		}
	}
}

// TestWebhooks_GitHubDispatchKind covers the github_dispatch webhook kind:
// creation requires owner/repo/token, the token round-trips encrypted (it
// must never appear verbatim in storage or in any API response), and
// updating without a new token keeps the previously stored one.
func TestWebhooks_GitHubDispatchKind(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("ghw1@x.com", true)
	p := e.createProject("GHDispatch")

	// Missing required fields.
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", Events: []string{"content.publish"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing github_repo, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", GitHubRepo: "mon-site",
		Events: []string{"content.publish"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing github_token on create, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", GitHubRepo: "mon-site", GitHubToken: "ghp_secret123",
		Events: []string{"content.publish", "content.unpublish"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "ghp_secret123") {
		t.Fatalf("plaintext token must never appear in the API response, got: %s", rec.Body.String())
	}
	wh := decodeBody[storage.Webhook](t, rec)

	// The token is encrypted at rest, never stored verbatim.
	stored, err := e.server.System.GetWebhook(bgCtx(), wh.ID)
	if err != nil {
		t.Fatalf("get webhook: %v", err)
	}
	if strings.Contains(stored.Config, "ghp_secret123") {
		t.Fatalf("token must be encrypted at rest, got config: %s", stored.Config)
	}
	decrypted, err := e.server.Encryptor.Decrypt(extractConfigToken(t, stored.Config))
	if err != nil || decrypted != "ghp_secret123" {
		t.Fatalf("expected stored token to decrypt back to the original, got %q (err=%v)", decrypted, err)
	}

	// Update without a token keeps the previous one, but does update owner/repo.
	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/webhooks/"+wh.ID, admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "louis", GitHubRepo: "mon-site-v2",
		Events: []string{"content.publish"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	stored2, _ := e.server.System.GetWebhook(bgCtx(), wh.ID)
	decrypted2, err := e.server.Encryptor.Decrypt(extractConfigToken(t, stored2.Config))
	if err != nil || decrypted2 != "ghp_secret123" {
		t.Fatalf("expected token to be preserved across an update that omits it, got %q (err=%v)", decrypted2, err)
	}

	// Unknown kind is rejected.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "carrier_pigeon", Events: []string{"content.publish"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown kind, got %d", rec.Code)
	}
}

// TestWebhooks_ValidationEdgeCases exercises the request-validation paths
// of buildWebhookFields/handleCreateWebhook/handleUpdateWebhook that
// TestWebhooks_AdminOnlyCRUD and TestWebhooks_GitHubDispatchKind don't
// already cover: malformed JSON bodies, missing events, missing
// generic-kind fields, updating with an invalid payload, and creating a
// github_dispatch webhook with no encryption key configured.
func TestWebhooks_ValidationEdgeCases(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("wve1@x.com", true)
	p := e.createProject("Validation")

	// Malformed JSON body (a bare JSON string, not an object) on create.
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, "not-an-object")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body on create, got %d", rec.Code)
	}

	// No events at all: defaults to every known event type rather than
	// being rejected, since a webhook is meant to fire on everything unless
	// the caller deliberately narrows it down.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "generic", URL: "https://example.com", Secret: "s",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for missing events (defaults to all), got %d", rec.Code)
	}
	whAllEvents := decodeBody[storage.Webhook](t, rec)
	if len(whAllEvents.Events) != len(availableWebhookEvents()) {
		t.Fatalf("expected webhook with omitted events to default to all %d events, got %d: %v",
			len(availableWebhookEvents()), len(whAllEvents.Events), whAllEvents.Events)
	}

	// Generic kind missing url/secret.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "generic", Events: []string{"content.create"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing url/secret on kind=generic, got %d", rec.Code)
	}

	// A valid webhook to update afterwards.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "generic", URL: "https://example.com", Secret: "s", Events: []string{"content.create"},
	})
	wh := decodeBody[storage.Webhook](t, rec)

	// Malformed JSON body on update.
	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/webhooks/"+wh.ID, admin, "not-an-object")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body on update, got %d", rec.Code)
	}

	// Validation failure on update (unknown kind).
	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/webhooks/"+wh.ID, admin, webhookRequest{
		Kind: "smoke_signal", Events: []string{"content.create"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown kind on update, got %d", rec.Code)
	}

	// Update targeting a non-existent webhook.
	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/webhooks/does-not-exist", admin, webhookRequest{
		Kind: "generic", URL: "https://example.com", Secret: "s", Events: []string{"content.create"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for updating a non-existent webhook, got %d", rec.Code)
	}

	// Deleting a non-existent webhook.
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/webhooks/does-not-exist", admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for deleting a non-existent webhook, got %d", rec.Code)
	}

	// github_dispatch requires a server-side encryption key.
	e.server.Encryptor = nil
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		Kind: "github_dispatch", GitHubOwner: "o", GitHubRepo: "r", GitHubToken: "t",
		Events: []string{"content.publish"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no Encryptor is configured, got %d: %s", rec.Code, rec.Body.String())
	}
}

// extractConfigToken pulls the (encrypted) token field out of a
// webhooks.GitHubDispatchConfig JSON blob for test assertions.
func extractConfigToken(t *testing.T, config string) string {
	t.Helper()
	var cfg struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return cfg.Token
}

// TestProjectLogs_AdminOnly guards the audit log being scoped to one
// project (not a cross-project, global list) while staying admin-only, per
// GET /api/v1/projects/{projectID}/logs (mirrors Tokens/Webhooks).
func TestProjectLogs_AdminOnly(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("l1@x.com", true)
	regular := e.createUser("l2@x.com", false)
	p := e.createProject("Logged")
	e.setRole(regular.ID, p.ID, storage.RoleConcepteur)

	// Even a CONCEPTEUR on the project cannot see its audit log.
	rec := e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/logs", regular, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// Any project-scoped action recorded against p.ID should show up here.
	target := e.createUser("l3@x.com", false)
	e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/users", admin, assignProjectUserRequest{
		Email: target.Email, Role: storage.RoleRedacteur,
	})

	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/logs", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	logs := decodeBody[[]storage.GlobalLog](t, rec)
	if len(logs) == 0 {
		t.Fatal("expected at least one log entry (project_permission.set)")
	}
	for _, l := range logs {
		if l.ProjectID != p.ID {
			t.Fatalf("expected all logs scoped to project %s, got %+v", p.ID, l)
		}
	}

	// A second, unrelated project must not see this project's log entries.
	other := e.createProject("Other")
	rec = e.request(http.MethodGet, "/api/v1/projects/"+other.ID+"/logs", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	otherLogs := decodeBody[[]storage.GlobalLog](t, rec)
	if len(otherLogs) != 0 {
		t.Fatalf("expected no log entries leaked into unrelated project, got %+v", otherLogs)
	}
}
