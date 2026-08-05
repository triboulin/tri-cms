package api

import (
	"net/http"
	"testing"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

func TestAPITokens_AdminOnlyAndTokenGrantsProjectAccess(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("t1@x.com", true)
	concepteur := e.createUser("t2@x.com", false)
	p := e.createProject("API Project")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	// Non-admin cannot create tokens.
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/tokens", concepteur, createTokenRequest{Name: "CI"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/tokens", admin, createTokenRequest{Name: "CI"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[createTokenResponse](t, rec)
	if created.Token == "" {
		t.Fatal("expected plaintext token to be returned once")
	}

	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/tokens", admin, nil)
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

	// A token cannot be used against a *different* project.
	other := e.createProject("Other")
	rec = e.requestWithToken(http.MethodGet, "/api/v1/projects/"+other.ID+"/contents/item", created.Token, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for token scoped to a different project, got %d", rec.Code)
	}

	// Revoke.
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/tokens/"+created.ID, admin, nil)
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

func TestGlobalLogs_AdminOnly(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("l1@x.com", true)
	regular := e.createUser("l2@x.com", false)

	rec := e.request(http.MethodGet, "/api/v1/logs", regular, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	e.request(http.MethodPost, "/api/v1/projects", admin, createProjectRequest{Name: "Logged"})

	rec = e.request(http.MethodGet, "/api/v1/logs", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	logs := decodeBody[[]storage.GlobalLog](t, rec)
	if len(logs) == 0 {
		t.Fatal("expected at least one log entry (project.create)")
	}
}
