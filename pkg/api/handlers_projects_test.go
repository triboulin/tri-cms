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

func TestDeleteProject_RequiresExactNameConfirmation(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("admin3@x.com", true)
	p := e.createProject("Sensitive Project")

	rec := e.request(http.MethodDelete, "/api/v1/projects/"+p.ID, admin, deleteProjectRequest{ConfirmName: "wrong name"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mismatched confirm_name, got %d", rec.Code)
	}

	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID, admin, deleteProjectRequest{ConfirmName: "Sensitive Project"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := e.server.System.GetProject(bgCtx(), p.ID); err == nil {
		t.Fatal("expected project to be deleted from system db")
	}
}

func TestDeleteProject_NonAdminForbidden(t *testing.T) {
	e := newTestEnv(t)
	regular := e.createUser("user3@x.com", false)
	p := e.createProject("P")
	e.setRole(regular.ID, p.ID, "CONCEPTEUR")

	rec := e.request(http.MethodDelete, "/api/v1/projects/"+p.ID, regular, deleteProjectRequest{ConfirmName: "P"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, even for CONCEPTEUR, got %d", rec.Code)
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
