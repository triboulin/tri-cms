package api

import (
	"net/http"
	"testing"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

func setupSchema(t *testing.T, e *testEnv, projectID string, concepteur *storage.User, slug string, def trischema.Definition) {
	t.Helper()
	rec := e.request(http.MethodPost, "/api/v1/projects/"+projectID+"/schemas", concepteur, schemaRequest{Slug: slug, Name: slug, Definition: def})
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup schema %q failed: %d %s", slug, rec.Code, rec.Body.String())
	}
}

func TestContentCRUD_FullLifecycle(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("c3@x.com", false)
	redacteur := e.createUser("r3@x.com", false)
	p := e.createProject("News")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	setupSchema(t, e, p.ID, concepteur, "article", trischema.Definition{Fields: []trischema.Field{
		{Key: "title", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
		{Key: "slug", Type: trischema.Slug, Cardinality: trischema.Simple, Required: true},
		{Key: "tags", Type: trischema.Text, Cardinality: trischema.Collection},
	}})

	// REDACTEUR can create content.
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/article", redacteur, contentRequest{
		Data: map[string]any{"title": "Hello World", "slug": "hello-world", "tags": []any{"go", "cms"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[contentResponse](t, rec)
	if created.Status != storage.StatusDraft {
		t.Fatalf("expected default draft status, got %s", created.Status)
	}
	if _, ok := created.Data["created_at"]; !ok {
		t.Fatal("expected root-level created_at to be stamped")
	}

	// Duplicate slug rejected.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/article", redacteur, contentRequest{
		Data: map[string]any{"title": "Another", "slug": "hello-world"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate slug, got %d: %s", rec.Code, rec.Body.String())
	}

	// Missing required field rejected.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/article", redacteur, contentRequest{
		Data: map[string]any{"slug": "no-title"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required field, got %d", rec.Code)
	}

	// Read.
	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/contents/article/"+created.ID, redacteur, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Update: change status to published.
	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/contents/article/"+created.ID, redacteur, contentRequest{
		Data:   map[string]any{"title": "Hello World Updated", "slug": "hello-world", "tags": []any{}},
		Status: storage.StatusPublished,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated := decodeBody[contentResponse](t, rec)
	if updated.Status != storage.StatusPublished {
		t.Fatalf("expected published status, got %s", updated.Status)
	}
	if updated.Data["created_at"] != created.Data["created_at"] {
		t.Fatal("expected created_at preserved across update")
	}

	// List.
	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/contents/article", redacteur, nil)
	list := decodeBody[[]contentResponse](t, rec)
	if len(list) != 1 {
		t.Fatalf("expected 1 content, got %d", len(list))
	}

	// Delete.
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/contents/article/"+created.ID, redacteur, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestContent_RoleEnforcement(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("c4@x.com", false)
	p := e.createProject("Store")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	setupSchema(t, e, p.ID, concepteur, "product", trischema.Definition{Fields: []trischema.Field{
		{Key: "name", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
	}})

	// No role at all on the project: forbidden.
	outsider := e.createUser("outsider@x.com", false)
	rec := e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/contents/product", outsider, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for user with no project role, got %d", rec.Code)
	}

	// CONCEPTEUR (inherits REDACTEUR) can create content too.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/product", concepteur, contentRequest{Data: map[string]any{"name": "Widget"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContent_ReferenceAndDeletionGuard(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("c5@x.com", false)
	redacteur := e.createUser("r5@x.com", false)
	p := e.createProject("Docs")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	setupSchema(t, e, p.ID, concepteur, "author", trischema.Definition{Fields: []trischema.Field{
		{Key: "name", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
	}})
	setupSchema(t, e, p.ID, concepteur, "post", trischema.Definition{Fields: []trischema.Field{
		{Key: "title", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
		{Key: "author", Type: trischema.Reference, Cardinality: trischema.Simple, TargetSchema: "author"},
	}})

	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/author", redacteur, contentRequest{Data: map[string]any{"name": "Ada"}})
	author := decodeBody[contentResponse](t, rec)

	// Reference to unknown content rejected.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/post", redacteur, contentRequest{
		Data: map[string]any{"title": "Post 1", "author": "does-not-exist"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for dangling reference, got %d: %s", rec.Code, rec.Body.String())
	}

	// Valid reference accepted.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/post", redacteur, contentRequest{
		Data: map[string]any{"title": "Post 1", "author": author.ID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Deleting the referenced author without force is refused.
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/contents/author/"+author.ID, redacteur, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for referenced content deletion, got %d", rec.Code)
	}

	// With force=true it proceeds.
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/contents/author/"+author.ID+"?force=true", redacteur, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with force=true, got %d", rec.Code)
	}
}
