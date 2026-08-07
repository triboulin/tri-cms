package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"tricms/pkg/storage"
)

func uploadFileForm(t *testing.T, e *testEnv, path string, u *storage.User, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if u != nil {
		req.AddCookie(e.sessionCookie(u))
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func TestHTMX_MediaUploadAndDelete(t *testing.T) {
	e := newHTMXTestEnv(t)
	redacteur := e.createUser("hm1@x.com", false)
	p := e.createProject("HTMXMedia")
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	rec := uploadFileForm(t, e, "/projects/"+p.ID+"/medias/upload", redacteur, "logo.png", []byte("fake-bytes"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	db, err := e.server.Manager.OpenProjectStorage(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	medias, err := db.ListMedias(bgCtx())
	if err != nil || len(medias) != 1 || medias[0].Filename != "logo.png" {
		t.Fatalf("expected 1 media uploaded: %v %+v", err, medias)
	}

	listPage := e.getHTML("/projects/"+p.ID+"/medias", redacteur)
	if !strings.Contains(listPage.Body.String(), "logo.png") {
		t.Fatalf("expected media listed, got %s", listPage.Body.String())
	}

	rec = e.postForm("/projects/"+p.ID+"/medias/"+medias[0].ID+"/delete", redacteur, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	medias, _ = db.ListMedias(bgCtx())
	if len(medias) != 0 {
		t.Fatalf("expected media deleted, got %d", len(medias))
	}
}

// TestHTMX_TokensPage_PlainGetDoesNotCrash guards against a regression where
// the template unconditionally read .Content.RevealedToken, a field that
// only existed on the Content struct built right after a create -- a plain
// GET (the common case, listing existing tokens) used a different struct
// without that field and crashed with a 500.
func TestHTMX_TokensPage_PlainGetDoesNotCrash(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("ht0@x.com", true)
	p := e.createProject("HTMXTokensPlainGet")

	rec := e.getHTML("/projects/"+p.ID+"/tokens", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on plain tokens GET, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHTMX_TokensPage_InteractiveAPIDocs guards the short interactive API
// documentation on the tokens page: a try-it console plus a quick-reference
// table of real, project-scoped endpoint paths.
func TestHTMX_TokensPage_InteractiveAPIDocs(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("htd0@x.com", true)
	p := e.createProject("HTMXTokensDocs")

	rec := e.getHTML("/projects/"+p.ID+"/tokens", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="api-console-method"`, `id="api-console-path"`, `id="api-console-send"`,
		`class="tri-api-doc-row"`,
		`/api/v1/projects/` + p.ID + `/schemas`,
		`/api/v1/projects/` + p.ID + `/contents/{slug}`,
		`/static/js/api-console.js`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected tokens page to contain %q, got:\n%s", want, body)
		}
	}
}

func TestHTMX_TokensCreateAndDelete(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("ht1@x.com", true)
	concepteur := e.createUser("ht2@x.com", false)
	p := e.createProject("HTMXTokens")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	// Non-admin forbidden (tokens are ADMIN-only per spec §1).
	rec := e.postForm("/projects/"+p.ID+"/tokens/create", concepteur, url.Values{"name": {"CI"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	rec = e.postForm("/projects/"+p.ID+"/tokens/create", admin, url.Values{"name": {"CI"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (reveal page, not a redirect), got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "trk_") {
		t.Fatalf("expected plaintext token revealed once, got %s", rec.Body.String())
	}

	tokens, err := e.server.System.ListAPITokens(bgCtx(), p.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("expected 1 token persisted: %v (%d)", err, len(tokens))
	}

	rec = e.postForm("/projects/"+p.ID+"/tokens/"+tokens[0].ID+"/delete", admin, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	tokens, _ = e.server.System.ListAPITokens(bgCtx(), p.ID)
	if len(tokens) != 0 {
		t.Fatalf("expected token revoked, got %d", len(tokens))
	}
}

func TestHTMX_WebhooksCreateUpdateDelete(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("hw1@x.com", true)
	concepteur := e.createUser("hw2@x.com", false)
	p := e.createProject("HTMXWebhooks")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	// Non-admin forbidden.
	rec := e.postForm("/projects/"+p.ID+"/webhooks/create", concepteur, url.Values{
		"url": {"https://example.com"}, "secret": {"s"}, "events": {"content.create"},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	rec = e.postForm("/projects/"+p.ID+"/webhooks/create", admin, url.Values{
		"url": {"https://example.com/hook"}, "secret": {"s3cret"}, "events": {"content.create", "content.delete"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	whs, err := e.server.System.ListWebhooks(bgCtx(), p.ID)
	if err != nil || len(whs) != 1 || len(whs[0].Events) != 2 {
		t.Fatalf("expected 1 webhook with 2 events: %v %+v", err, whs)
	}

	page := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	if !strings.Contains(page.Body.String(), "example.com/hook") {
		t.Fatalf("expected webhook listed, got %s", page.Body.String())
	}

	rec = e.postForm("/projects/"+p.ID+"/webhooks/"+whs[0].ID+"/update", admin, url.Values{
		"url": {"https://example.com/hook2"}, "secret": {"s2"}, "events": {"media.create"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, _ := e.server.System.GetWebhook(bgCtx(), whs[0].ID)
	if updated.URL != "https://example.com/hook2" || len(updated.Events) != 1 {
		t.Fatalf("expected webhook updated: %+v", updated)
	}

	rec = e.postForm("/projects/"+p.ID+"/webhooks/"+whs[0].ID+"/delete", admin, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	whs, _ = e.server.System.ListWebhooks(bgCtx(), p.ID)
	if len(whs) != 0 {
		t.Fatalf("expected webhook deleted, got %d", len(whs))
	}
}

// TestHTMX_WebhooksGitHubDispatchKind covers creating/editing a
// github_dispatch webhook through the admin form (as opposed to the JSON
// API, covered by TestWebhooks_GitHubDispatchKind): the rendered page must
// show the configured owner/repo but never the token, and the create form
// must reject an incomplete submission.
func TestHTMX_WebhooksGitHubDispatchKind(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("ghwh1@x.com", true)
	p := e.createProject("HTMXGHDispatch")

	rec := e.postForm("/projects/"+p.ID+"/webhooks/create", admin, url.Values{
		"kind": {"github_dispatch"}, "github_owner": {"louis"}, "github_repo": {"mon-site"},
		"github_token": {"ghp_topsecret"}, "events": {"content.publish"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	whs, err := e.server.System.ListWebhooks(bgCtx(), p.ID)
	if err != nil || len(whs) != 1 || whs[0].Kind != "github_dispatch" {
		t.Fatalf("expected 1 github_dispatch webhook: %v %+v", err, whs)
	}

	page := e.getHTML("/projects/"+p.ID+"/webhooks", admin)
	body := page.Body.String()
	if !strings.Contains(body, `value="louis"`) || !strings.Contains(body, `value="mon-site"`) {
		t.Fatalf("expected owner/repo prefilled in the edit form, got %s", body)
	}
	if strings.Contains(body, "ghp_topsecret") {
		t.Fatalf("token must never be rendered back into the page, got %s", body)
	}

	// Missing owner/repo is rejected with a flash error, not a crash.
	rec = e.postForm("/projects/"+p.ID+"/webhooks/create", admin, url.Values{
		"kind": {"github_dispatch"}, "github_token": {"x"}, "events": {"content.publish"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect (with flash error), got %d", rec.Code)
	}
	whs2, _ := e.server.System.ListWebhooks(bgCtx(), p.ID)
	if len(whs2) != 1 {
		t.Fatalf("expected the invalid submission to be rejected, still 1 webhook, got %d", len(whs2))
	}
}

// TestHTMX_Webhooks_ValidationAndAuthEdgeCases covers the remaining
// htmx_webhooks.go branches not exercised by the two tests above:
// non-admin access to the list page, updating a non-existent webhook, and
// submitting an invalid update.
func TestHTMX_Webhooks_ValidationAndAuthEdgeCases(t *testing.T) {
	e := newHTMXTestEnv(t)
	admin := e.createUser("ghwh2@x.com", true)
	concepteur := e.createUser("ghwh3@x.com", false)
	p := e.createProject("HTMXWebhooksEdge")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	// Non-admin cannot view the webhooks page.
	page := e.getHTML("/projects/"+p.ID+"/webhooks", concepteur)
	if page.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin viewing webhooks page, got %d", page.Code)
	}

	// Updating a webhook that doesn't exist -- writeHTMXStorageError maps
	// storage.ErrNotFound to a plain 404, same as other HTMX resources.
	rec := e.postForm("/projects/"+p.ID+"/webhooks/does-not-exist/update", admin, url.Values{
		"kind": {"generic"}, "url": {"https://example.com"}, "secret": {"s"}, "events": {"content.create"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for updating a missing webhook, got %d", rec.Code)
	}

	// A valid webhook, then an invalid update to it (unknown kind).
	rec = e.postForm("/projects/"+p.ID+"/webhooks/create", admin, url.Values{
		"kind": {"generic"}, "url": {"https://example.com"}, "secret": {"s"}, "events": {"content.create"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	whs, _ := e.server.System.ListWebhooks(bgCtx(), p.ID)
	if len(whs) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(whs))
	}
	rec = e.postForm("/projects/"+p.ID+"/webhooks/"+whs[0].ID+"/update", admin, url.Values{
		"kind": {"smoke_signal"}, "events": {"content.create"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect (with flash error) for invalid update, got %d", rec.Code)
	}
	unchanged, _ := e.server.System.GetWebhook(bgCtx(), whs[0].ID)
	if unchanged.Kind != "generic" {
		t.Fatalf("expected webhook untouched after rejected update, got kind=%q", unchanged.Kind)
	}
}
