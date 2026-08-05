package api

import (
	"net/http"
	"testing"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

func TestSchemaCRUD_RoleEnforcement(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("c@x.com", false)
	redacteur := e.createUser("r@x.com", false)
	p := e.createProject("Blog")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	def := trischema.Definition{Fields: []trischema.Field{
		{Key: "title", Label: "Title", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
	}}

	// REDACTEUR cannot create schemas.
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/schemas", redacteur, schemaRequest{Slug: "article", Name: "Article", Definition: def})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for REDACTEUR creating schema, got %d", rec.Code)
	}

	// CONCEPTEUR can.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/schemas", concepteur, schemaRequest{Slug: "article", Name: "Article", Definition: def})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Duplicate slug conflicts.
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/schemas", concepteur, schemaRequest{Slug: "article", Name: "Article2", Definition: def})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}

	// REDACTEUR can still read.
	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/schemas", redacteur, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Invalid definition rejected.
	badDef := trischema.Definition{Fields: []trischema.Field{{Key: "x", Type: "Bogus", Cardinality: trischema.Simple}}}
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/schemas", concepteur, schemaRequest{Slug: "bad", Name: "Bad", Definition: badDef})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid definition, got %d", rec.Code)
	}

	// Update (slug immutable, only name/definition change).
	def2 := trischema.Definition{Fields: []trischema.Field{
		{Key: "title", Label: "Title", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
		{Key: "body", Label: "Body", Type: trischema.RichTextMD, Cardinality: trischema.Simple},
	}}
	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/schemas/article", concepteur, schemaRequest{Name: "News Article", Definition: def2})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete requires CONCEPTEUR+.
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/schemas/article", redacteur, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/schemas/article", concepteur, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestFolders_RoleEnforcement(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("c2@x.com", false)
	redacteur := e.createUser("r2@x.com", false)
	p := e.createProject("Site")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/folders", redacteur, folderRequest{Name: "News"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/folders", concepteur, folderRequest{Name: "News"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	folder := decodeBody[storage.Folder](t, rec)

	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/folders", redacteur, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/folders/"+folder.ID, concepteur, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}
