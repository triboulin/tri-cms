package api

import (
	"net/http"
	"testing"

	"tricms/pkg/storage"
)

func TestGlobalUsers_AdminOnlyCRUD(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("root@x.com", true)
	regular := e.createUser("plain@x.com", false)

	// Non-admin forbidden everywhere.
	if rec := e.request(http.MethodGet, "/api/v1/users", regular, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	rec := e.request(http.MethodPost, "/api/v1/users", admin, createUserRequest{Email: "new@x.com", Password: "secret123"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[userView](t, rec)

	rec = e.request(http.MethodGet, "/api/v1/users", admin, nil)
	list := decodeBody[[]userView](t, rec)
	if len(list) != 3 {
		t.Fatalf("expected 3 users, got %d", len(list))
	}

	adminFlag := true
	rec = e.request(http.MethodPatch, "/api/v1/users/"+created.ID, admin, updateUserRequest{IsGlobalAdmin: &adminFlag})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	updated := decodeBody[userView](t, rec)
	if !updated.IsGlobalAdmin {
		t.Fatal("expected user promoted to global admin")
	}

	rec = e.request(http.MethodDelete, "/api/v1/users/"+created.ID, admin, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if _, err := e.server.System.GetUserByID(bgCtx(), created.ID); err == nil {
		t.Fatal("expected user to be deleted")
	}
}

func TestCreateUser_RequiresEmailAndPassword(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("admin5@x.com", true)
	rec := e.request(http.MethodPost, "/api/v1/users", admin, createUserRequest{Email: "", Password: ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestProjectUsers_AssignmentRules(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("admin6@x.com", true)
	gestionnaire := e.createUser("gest@x.com", false)
	concepteur := e.createUser("concept@x.com", false)
	target := e.createUser("target@x.com", false)

	p := e.createProject("Proj")
	e.setRole(gestionnaire.ID, p.ID, storage.RoleGestionnaire)
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	// GESTIONNAIRE can assign REDACTEUR.
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/users", gestionnaire, assignProjectUserRequest{Email: target.Email, Role: storage.RoleRedacteur})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// GESTIONNAIRE cannot assign CONCEPTEUR.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/users", gestionnaire, assignProjectUserRequest{Email: target.Email, Role: storage.RoleConcepteur})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for GESTIONNAIRE assigning CONCEPTEUR, got %d", rec.Code)
	}

	// CONCEPTEUR (inherits GESTIONNAIRE rights only for this action) also cannot assign CONCEPTEUR.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/users", concepteur, assignProjectUserRequest{Email: target.Email, Role: storage.RoleConcepteur})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for CONCEPTEUR assigning CONCEPTEUR via users view, got %d", rec.Code)
	}

	// Only ADMIN can assign CONCEPTEUR.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/users", admin, assignProjectUserRequest{Email: target.Email, Role: storage.RoleConcepteur})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin assigning CONCEPTEUR, got %d: %s", rec.Code, rec.Body.String())
	}

	// REDACTEUR cannot access the users view at all.
	redacteur := e.createUser("red@x.com", false)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)
	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/users", redacteur, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for REDACTEUR listing project users, got %d", rec.Code)
	}

	// List as GESTIONNAIRE succeeds.
	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/users", gestionnaire, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Removing a CONCEPTEUR's permission requires CONCEPTEUR+/admin.
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/users/"+target.ID, gestionnaire, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403: GESTIONNAIRE cannot remove a CONCEPTEUR-level permission, got %d", rec.Code)
	}
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/users/"+target.ID, admin, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}
