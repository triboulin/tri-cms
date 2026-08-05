package api

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

func loadTestTemplates(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.ParseGlob("../../web/templates/*.html")
	if err != nil {
		t.Fatalf("parse partials: %v", err)
	}
	tmpl, err = tmpl.ParseGlob("../../web/templates/pages/*.html")
	if err != nil {
		t.Fatalf("parse pages: %v", err)
	}
	return tmpl
}

// newHTMXTestEnv is like newTestEnv but also wires the real templates and a
// static file server, exercising the full HTMX admin UI end to end.
func newHTMXTestEnv(t *testing.T) *testEnv {
	e := newTestEnv(t)
	e.server.Templates = loadTestTemplates(t)
	e.server.StaticFS = os.DirFS("../../web/static")
	e.router = NewRouter(e.server)
	return e
}

func (e *testEnv) getHTML(path string, u *storage.User) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if u != nil {
		req.AddCookie(e.sessionCookie(u))
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func TestHTMX_LoginPage(t *testing.T) {
	e := newHTMXTestEnv(t)
	rec := e.getHTML("/login", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Se connecter") {
		t.Fatalf("expected login form in body, got %s", rec.Body.String())
	}
}

func TestHTMX_DashboardRedirectsAnonymous(t *testing.T) {
	e := newHTMXTestEnv(t)
	rec := e.getHTML("/", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect to login, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("expected redirect to /login, got %q", loc)
	}
}

func TestHTMX_LoginSubmitSetsCookieAndRedirects(t *testing.T) {
	e := newHTMXTestEnv(t)
	e.createUser("htmxuser@x.com", false)

	form := "email=htmxuser%40x.com&password=password123"
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie to be set")
	}
}

func TestHTMX_LoginSubmitWrongPassword(t *testing.T) {
	e := newHTMXTestEnv(t)
	e.createUser("htmxuser2@x.com", false)
	form := "email=htmxuser2%40x.com&password=wrong"
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 re-rendering login with error, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Identifiants invalides") {
		t.Fatalf("expected error message, got %s", rec.Body.String())
	}
}

func TestHTMX_Dashboard_ListsProjects(t *testing.T) {
	e := newHTMXTestEnv(t)
	u := e.createUser("dash@x.com", false)
	p := e.createProject("Dashboard Project")
	e.setRole(u.ID, p.ID, storage.RoleRedacteur)

	rec := e.getHTML("/", u)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Dashboard Project") {
		t.Fatalf("expected project listed, got %s", rec.Body.String())
	}
}

func TestHTMX_Logout(t *testing.T) {
	e := newHTMXTestEnv(t)
	u := e.createUser("logout@x.com", false)
	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	req.AddCookie(e.sessionCookie(u))
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
}

func TestHTMX_ProjectSections_RoleGating(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("hconcept@x.com", false)
	redacteur := e.createUser("hred@x.com", false)
	outsider := e.createUser("hout@x.com", false)
	p := e.createProject("Gated Project")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	setupSchema(t, e, p.ID, concepteur, "page", trischema.Definition{Fields: []trischema.Field{
		{Key: "title", Type: trischema.Text, Cardinality: trischema.Simple},
	}})

	// Collections: available to REDACTEUR.
	rec := e.getHTML("/projects/"+p.ID, redacteur)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "page") {
		t.Fatalf("expected 200 with schema listed, got %d: %s", rec.Code, rec.Body.String())
	}

	// Users view: forbidden for REDACTEUR, allowed for CONCEPTEUR.
	rec = e.getHTML("/projects/"+p.ID+"/users", redacteur)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for REDACTEUR on users view, got %d", rec.Code)
	}
	rec = e.getHTML("/projects/"+p.ID+"/users", concepteur)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for CONCEPTEUR on users view, got %d", rec.Code)
	}

	// Outsider (no role) is forbidden everywhere in the project.
	rec = e.getHTML("/projects/"+p.ID, outsider)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for outsider, got %d", rec.Code)
	}

	// Medias page reachable by REDACTEUR.
	rec = e.getHTML("/projects/"+p.ID+"/medias", redacteur)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for medias, got %d", rec.Code)
	}

	// Unknown project -> 404.
	rec = e.getHTML("/projects/does-not-exist", redacteur)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHTMX_AdminPages_GlobalAdminOnly(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("hadmin@x.com", true)
	regular := e.createUser("hregular@x.com", false)
	e.createProject("Visible To Admin")

	for _, path := range []string{"/admin", "/admin/projects", "/admin/users", "/admin/logs"} {
		rec := e.getHTML(path, regular)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 403/redirect for non-admin at %s, got %d", path, rec.Code)
		}
	}

	rec := e.getHTML("/admin", admin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected /admin to redirect, got %d", rec.Code)
	}
	rec = e.getHTML("/admin/projects", admin)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Visible To Admin") {
		t.Fatalf("expected 200 listing project, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = e.getHTML("/admin/users", admin)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hadmin@x.com") {
		t.Fatalf("expected 200 listing users, got %d", rec.Code)
	}
	rec = e.getHTML("/admin/logs", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for logs, got %d", rec.Code)
	}
}

func TestHTMX_StaticFileServed(t *testing.T) {
	e := newHTMXTestEnv(t)
	rec := e.getHTML("/static/css/app.css", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for static css, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "tri-app") {
		t.Fatalf("expected css content, got %s", rec.Body.String())
	}
}

func TestHTMX_AlreadyLoggedInRedirectsFromLogin(t *testing.T) {
	e := newHTMXTestEnv(t)
	u := e.createUser("already@x.com", false)
	rec := e.getHTML("/login", u)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect for already-authenticated user, got %d", rec.Code)
	}
}

func TestRender_UnknownTemplateReturns500(t *testing.T) {
	e := newHTMXTestEnv(t)
	rec := httptest.NewRecorder()
	e.server.render(rec, "page:does-not-exist", &PageData{})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unknown template, got %d", rec.Code)
	}
}

func TestHTMX_ProjectPages_AnonymousRedirectToLogin(t *testing.T) {
	e := newHTMXTestEnv(t)
	p := e.createProject("Anon")
	for _, path := range []string{"/projects/" + p.ID, "/projects/" + p.ID + "/medias", "/projects/" + p.ID + "/users"} {
		rec := e.getHTML(path, nil)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected redirect to login for %s, got %d", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("expected /login redirect for %s, got %q", path, loc)
		}
	}
}
