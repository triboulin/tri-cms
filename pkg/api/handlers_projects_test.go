package api

import (
	"net/http"
	"testing"
)

func TestCreateProject_AdminOnly(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("admin@x.com", true)
	regular := e.createUser("user@x.com", false)

	rec := e.request(http.MethodPost, "/api/v1/projects", regular, createProjectRequest{Name: "Acme"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}

	rec = e.request(http.MethodPost, "/api/v1/projects", admin, createProjectRequest{Name: "Acme"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[projectView](t, rec)
	if got.Name != "Acme" || got.ID == "" {
		t.Fatalf("unexpected project view: %+v", got)
	}
}

func TestListProjects_ScopedToUser(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("admin2@x.com", true)
	regular := e.createUser("user2@x.com", false)

	p1 := e.createProject("P1")
	e.createProject("P2")
	e.setRole(regular.ID, p1.ID, "REDACTEUR")

	rec := e.request(http.MethodGet, "/api/v1/projects", admin, nil)
	list := decodeBody[[]projectView](t, rec)
	if len(list) != 2 {
		t.Fatalf("expected admin to see 2 projects, got %d", len(list))
	}

	rec = e.request(http.MethodGet, "/api/v1/projects", regular, nil)
	list = decodeBody[[]projectView](t, rec)
	if len(list) != 1 || list[0].ID != p1.ID {
		t.Fatalf("expected regular user to see only P1, got %+v", list)
	}
}

// TestDeleteProject_NoAPIRoute guards a deliberate design decision: unlike
// every other resource, project deletion has no JSON API route at all --
// not even for an admin session, and not for a token (which is
// ADMIN-equivalent for everything else). The only way to delete a project is
// the HTMX admin UI's double-confirmation flow (htmxAdminDeleteProject in
// htmx_admin.go), which a script cannot drive by accident. See
// pkg/api/router.go and pkg/api/handlers_projects.go for the removed-route
// rationale.
func TestDeleteProject_NoAPIRoute(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("admin3@x.com", true)
	p := e.createProject("Sensitive Project")

	rec := e.request(http.MethodDelete, "/api/v1/projects/"+p.ID, admin, nil)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected DELETE /api/v1/projects/{id} to not exist (404/405), got %d: %s", rec.Code, rec.Body.String())
	}

	// Even a global-admin token, which is ADMIN-equivalent for every other
	// action, cannot delete a project through the API either -- because the
	// route simply doesn't exist to be authorized against.
	tokenPlain := e.createToken(t, admin, "delete-test-token")
	rec = e.requestWithToken(http.MethodDelete, "/api/v1/projects/"+p.ID, tokenPlain, nil)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected DELETE /api/v1/projects/{id} to not exist via token either, got %d", rec.Code)
	}

	if _, err := e.server.System.GetProject(bgCtx(), p.ID); err != nil {
		t.Fatalf("expected project to still exist (no API route can delete it), got err: %v", err)
	}
}

func TestProjectContext_UnknownProject404(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("admin4@x.com", true)
	rec := e.request(http.MethodGet, "/api/v1/projects/does-not-exist/folders", admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
