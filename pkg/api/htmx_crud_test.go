package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"tricms/pkg/storage"
)

func TestHTMX_CreateProjectFromDashboard(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("dashadmin@x.com", true)
	regular := e.createUser("dashuser@x.com", false)

	// The reported pain point: project creation must be reachable from the
	// dashboard, not just the JSON API.
	rec := e.postForm("/projects/create", admin, url.Values{"name": {"Nouveau Site"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/projects/proj_") {
		t.Fatalf("expected redirect into the new project, got %q", loc)
	}

	projects, err := e.server.System.ListProjects(bgCtx())
	if err != nil || len(projects) != 1 || projects[0].Name != "Nouveau Site" {
		t.Fatalf("expected project persisted: %v %+v", err, projects)
	}

	// Non-admin forbidden.
	rec = e.postForm("/projects/create", regular, url.Values{"name": {"Nope"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}

	// Missing name redirects with an error flash, not a crash.
	rec = e.postForm("/projects/create", admin, url.Values{"name": {""}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected redirect with error flash, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHTMX_AdminDeleteProject_RequiresExactConfirmation(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("delp@x.com", true)
	p := e.createProject("Delete Me")

	rec := e.postForm("/admin/projects/"+p.ID+"/delete", admin, url.Values{"confirm_name": {"wrong"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected error flash on mismatch, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := e.server.System.GetProject(bgCtx(), p.ID); err != nil {
		t.Fatal("project should not have been deleted yet")
	}

	rec = e.postForm("/admin/projects/"+p.ID+"/delete", admin, url.Values{"confirm_name": {"Delete Me"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if _, err := e.server.System.GetProject(bgCtx(), p.ID); err == nil {
		t.Fatal("expected project deleted")
	}
}

func TestHTMX_AdminUsersCRUD(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("auadmin@x.com", true)

	rec := e.postForm("/admin/users/create", admin, url.Values{"email": {"new@x.com"}, "password": {"secret123"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	created, err := e.server.System.GetUserByEmail(bgCtx(), "new@x.com")
	if err != nil {
		t.Fatalf("expected user created: %v", err)
	}
	if created.IsGlobalAdmin {
		t.Fatal("expected non-admin by default")
	}

	rec = e.postForm("/admin/users/"+created.ID+"/toggle-admin", admin, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	updated, _ := e.server.System.GetUserByID(bgCtx(), created.ID)
	if !updated.IsGlobalAdmin {
		t.Fatal("expected toggled to admin")
	}

	rec = e.postForm("/admin/users/"+created.ID+"/delete", admin, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if _, err := e.server.System.GetUserByID(bgCtx(), created.ID); err == nil {
		t.Fatal("expected user deleted")
	}
}

func TestHTMX_AdminPermissionsMatrix(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("apm1@x.com", true)
	target := e.createUser("apm2@x.com", false)
	p := e.createProject("Matrix Project")

	// Only the global matrix can grant CONCEPTEUR.
	rec := e.postForm("/admin/permissions/assign", admin, url.Values{
		"project_id": {p.ID}, "email": {target.Email}, "role": {string(storage.RoleConcepteur)},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	pp, err := e.server.System.GetProjectPermission(bgCtx(), target.ID, p.ID)
	if err != nil || pp.Role != storage.RoleConcepteur {
		t.Fatalf("expected CONCEPTEUR permission: %v %+v", err, pp)
	}

	rec = e.postForm("/admin/permissions/"+p.ID+"/"+target.ID+"/remove", admin, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if _, err := e.server.System.GetProjectPermission(bgCtx(), target.ID, p.ID); err == nil {
		t.Fatal("expected permission removed")
	}

	page := e.getHTML("/admin/permissions", admin)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Matrix Project") {
		t.Fatalf("expected permissions page to list project, got %d", page.Code)
	}
}

func TestHTMX_ProjectUsers_AssignAndRemove(t *testing.T) {
	e := newHTMXTestEnv(t)
	gestionnaire := e.createUser("pug1@x.com", false)
	target := e.createUser("pug2@x.com", false)
	p := e.createProject("PU Project")
	e.setRole(gestionnaire.ID, p.ID, storage.RoleGestionnaire)

	rec := e.postForm("/projects/"+p.ID+"/users/assign", gestionnaire, url.Values{
		"email": {target.Email}, "role": {string(storage.RoleRedacteur)},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	pp, err := e.server.System.GetProjectPermission(bgCtx(), target.ID, p.ID)
	if err != nil || pp.Role != storage.RoleRedacteur {
		t.Fatalf("expected REDACTEUR assigned: %v %+v", err, pp)
	}

	// GESTIONNAIRE cannot grant CONCEPTEUR from this view.
	rec = e.postForm("/projects/"+p.ID+"/users/assign", gestionnaire, url.Values{
		"email": {target.Email}, "role": {string(storage.RoleConcepteur)},
	})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected error flash, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = e.postForm("/projects/"+p.ID+"/users/"+target.ID+"/remove", gestionnaire, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if _, err := e.server.System.GetProjectPermission(bgCtx(), target.ID, p.ID); err == nil {
		t.Fatal("expected permission removed")
	}
}

// TestHTMX_ProjectUsers_CreateAccountInline guards the ability to create a
// brand-new account directly from a project's Users page (previously only
// possible via the global-admin-only /admin/users/create, unreachable for a
// plain GESTIONNAIRE): assigning a role to an email with no existing account
// plus a password should create the account and grant the role in one step.
func TestHTMX_ProjectUsers_CreateAccountInline(t *testing.T) {
	e := newHTMXTestEnv(t)
	gestionnaire := e.createUser("puc1@x.com", false)
	p := e.createProject("PU CreateInline")
	e.setRole(gestionnaire.ID, p.ID, storage.RoleGestionnaire)

	const newEmail = "brandnew@x.com"

	// No password supplied and no existing account: rejected with a helpful
	// error rather than a confusing "no such user".
	rec := e.postForm("/projects/"+p.ID+"/users/assign", gestionnaire, url.Values{
		"email": {newEmail}, "role": {string(storage.RoleRedacteur)},
	})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected error flash without password, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := e.server.System.GetUserByEmail(bgCtx(), newEmail); err == nil {
		t.Fatal("account should not have been created")
	}

	// With a password: creates the account and assigns the role.
	rec = e.postForm("/projects/"+p.ID+"/users/assign", gestionnaire, url.Values{
		"email": {newEmail}, "password": {"s3cret-pass"}, "role": {string(storage.RoleRedacteur)},
	})
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected success, got %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	created, err := e.server.System.GetUserByEmail(bgCtx(), newEmail)
	if err != nil {
		t.Fatalf("expected account created: %v", err)
	}
	if created.IsGlobalAdmin {
		t.Fatal("account created from a project page must not be global admin")
	}
	pp, err := e.server.System.GetProjectPermission(bgCtx(), created.ID, p.ID)
	if err != nil || pp.Role != storage.RoleRedacteur {
		t.Fatalf("expected REDACTEUR assigned to new account: %v %+v", err, pp)
	}
}

// TestHTMX_ProjectUsers_UpdateRole guards the in-place role change (no more
// remove-then-reassign round trip) and the RBAC boundary that a
// GESTIONNAIRE cannot use it to touch a CONCEPTEUR-level member.
func TestHTMX_ProjectUsers_UpdateRole(t *testing.T) {
	e := newHTMXTestEnv(t)
	gestionnaire := e.createUser("pur1@x.com", false)
	target := e.createUser("pur2@x.com", false)
	concepteurUser := e.createUser("pur3@x.com", false)
	p := e.createProject("PU UpdateRole")
	e.setRole(gestionnaire.ID, p.ID, storage.RoleGestionnaire)
	e.setRole(target.ID, p.ID, storage.RoleRedacteur)
	e.setRole(concepteurUser.ID, p.ID, storage.RoleConcepteur)

	rec := e.postForm("/projects/"+p.ID+"/users/"+target.ID+"/role", gestionnaire, url.Values{
		"role": {string(storage.RoleGestionnaire)},
	})
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected success, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	pp, err := e.server.System.GetProjectPermission(bgCtx(), target.ID, p.ID)
	if err != nil || pp.Role != storage.RoleGestionnaire {
		t.Fatalf("expected GESTIONNAIRE, got %v %+v", err, pp)
	}

	// A GESTIONNAIRE cannot touch a CONCEPTEUR-level member via this route.
	rec = e.postForm("/projects/"+p.ID+"/users/"+concepteurUser.ID+"/role", gestionnaire, url.Values{
		"role": {string(storage.RoleRedacteur)},
	})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected error flash, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	pp, err = e.server.System.GetProjectPermission(bgCtx(), concepteurUser.ID, p.ID)
	if err != nil || pp.Role != storage.RoleConcepteur {
		t.Fatalf("expected CONCEPTEUR untouched, got %v %+v", err, pp)
	}
}

// TestHTMX_ProjectUsers_AdminSentenceHiddenFromNonAdmin guards the visibility
// of the sentence pointing at the global permissions matrix: it links to an
// admin-only page and previously showed to every viewer including plain
// GESTIONNAIRE users who can't follow it anyway.
func TestHTMX_ProjectUsers_AdminSentenceHiddenFromNonAdmin(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("pus1@x.com", true)
	gestionnaire := e.createUser("pus2@x.com", false)
	p := e.createProject("PU SentenceVisibility")
	e.setRole(gestionnaire.ID, p.ID, storage.RoleGestionnaire)

	const sentence = "matrice globale des droits"

	asAdmin := e.getHTML("/projects/"+p.ID+"/users", admin)
	if !strings.Contains(asAdmin.Body.String(), sentence) {
		t.Fatalf("expected admin to see the sentence, got %s", asAdmin.Body.String())
	}

	asGestionnaire := e.getHTML("/projects/"+p.ID+"/users", gestionnaire)
	if strings.Contains(asGestionnaire.Body.String(), sentence) {
		t.Fatalf("expected non-admin NOT to see the sentence, got %s", asGestionnaire.Body.String())
	}
}
