package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

func TestGetSchema_Success(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("gs1@x.com", false)
	p := e.createProject("GS")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	setupSchema(t, e, p.ID, concepteur, "page", trischema.Definition{Fields: []trischema.Field{
		{Key: "title", Type: trischema.Text, Cardinality: trischema.Simple},
	}})

	rec := e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/schemas/page", concepteur, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[storage.Schema](t, rec)
	if got.Slug != "page" {
		t.Fatalf("unexpected schema: %+v", got)
	}

	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/schemas/missing", concepteur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMalformedBodies_Return400(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("mb1@x.com", true)
	p := e.createProject("MB")

	badJSON := map[string]any{"unknown_field_xyz": true}

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"create folder", http.MethodPost, "/api/v1/projects/" + p.ID + "/folders"},
		{"create schema", http.MethodPost, "/api/v1/projects/" + p.ID + "/schemas"},
		{"create webhook", http.MethodPost, "/api/v1/projects/" + p.ID + "/webhooks"},
		{"create token", http.MethodPost, "/api/v1/projects/" + p.ID + "/tokens"},
		{"create user", http.MethodPost, "/api/v1/users"},
		{"assign project user", http.MethodPost, "/api/v1/projects/" + p.ID + "/users"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.request(c.method, c.path, admin, badJSON)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpdateUser_PasswordOnly(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("uu1@x.com", true)
	target := e.createUser("uu2@x.com", false)

	newPass := "brand-new-pass"
	rec := e.request(http.MethodPatch, "/api/v1/users/"+target.ID, admin, updateUserRequest{Password: &newPass})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Old password should no longer work, new one should.
	rec = e.request(http.MethodPost, "/api/v1/auth/login", nil, loginRequest{Email: target.Email, Password: "password123"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected old password rejected, got %d", rec.Code)
	}
	rec = e.request(http.MethodPost, "/api/v1/auth/login", nil, loginRequest{Email: target.Email, Password: newPass})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected new password accepted, got %d", rec.Code)
	}
}

func TestUpdateUser_UnknownUser404(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("uu3@x.com", true)
	flag := true
	rec := e.request(http.MethodPatch, "/api/v1/users/does-not-exist", admin, updateUserRequest{IsGlobalAdmin: &flag})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAssignProjectUser_UnknownEmail(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("apu1@x.com", true)
	p := e.createProject("APU")
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/users", admin, assignProjectUserRequest{Email: "ghost@x.com", Role: storage.RoleRedacteur})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAssignProjectUser_InvalidRole(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("apu2@x.com", true)
	target := e.createUser("apu3@x.com", false)
	p := e.createProject("APU2")
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/users", admin, assignProjectUserRequest{Email: target.Email, Role: storage.Role("BOGUS")})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRemoveProjectUser_UnknownPermission404(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("rpu1@x.com", true)
	target := e.createUser("rpu2@x.com", false)
	p := e.createProject("RPU")
	rec := e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/users/"+target.ID, admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAPIToken_InsufficientRoleForbidden(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("tok1@x.com", true)
	p := e.createProject("TokRole")
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/tokens", admin, createTokenRequest{Name: "limited"})
	tok := decodeBody[createTokenResponse](t, rec)

	// Tokens grant REDACTEUR-equivalent access only: schema creation (CONCEPTEUR+) must be refused.
	rec = e.requestWithToken(http.MethodPost, "/api/v1/projects/"+p.ID+"/schemas", tok.Token, schemaRequest{
		Slug: "x", Name: "X", Definition: trischema.Definition{Fields: []trischema.Field{{Key: "a", Type: trischema.Text, Cardinality: trischema.Simple}}},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for token attempting CONCEPTEUR-level action, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateProject_MissingName(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("cp1@x.com", true)
	rec := e.request(http.MethodPost, "/api/v1/projects", admin, createProjectRequest{Name: ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookDispatch_FiresOnContentCreate(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("wd1@x.com", false)
	admin := e.createUser("wd2@x.com", true)
	p := e.createProject("Dispatch")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	setupSchema(t, e, p.ID, concepteur, "note", trischema.Definition{Fields: []trischema.Field{
		{Key: "body", Type: trischema.Text, Cardinality: trischema.Simple},
	}})

	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("X-TriCMS-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/webhooks", admin, webhookRequest{
		URL: srv.URL, Secret: "s", Events: []string{"content.create"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/note", concepteur, contentRequest{Data: map[string]any{"body": "hi"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case ev := <-received:
		if ev != "content.create" {
			t.Fatalf("expected content.create event, got %q", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
}

func TestServer_CloseAndForgetProjectDB(t *testing.T) {
	e := newTestEnv(t)
	p := e.createProject("Closeable")
	if _, err := e.server.projectDB(p.ID); err != nil {
		t.Fatalf("open project db: %v", err)
	}
	e.server.forgetProjectDB(p.ID)
	if _, err := e.server.projectDB(p.ID); err != nil {
		t.Fatalf("reopen after forget: %v", err)
	}
	if err := e.server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
