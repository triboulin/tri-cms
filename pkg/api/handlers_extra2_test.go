package api

import (
	"net/http"
	"testing"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

func TestLogs_LimitParam(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("lg1@x.com", true)
	for i := 0; i < 3; i++ {
		e.request(http.MethodPost, "/api/v1/projects", admin, createProjectRequest{Name: "P"})
	}

	rec := e.request(http.MethodGet, "/api/v1/logs?limit=1", admin, nil)
	logs := decodeBody[[]storage.GlobalLog](t, rec)
	if len(logs) != 1 {
		t.Fatalf("expected limit=1 to return exactly 1 log, got %d", len(logs))
	}

	// Invalid limit falls back to the default rather than erroring.
	rec = e.request(http.MethodGet, "/api/v1/logs?limit=notanumber", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with invalid limit ignored, got %d", rec.Code)
	}
}

func TestMedia_DeleteUnknown404(t *testing.T) {
	e := newTestEnv(t)
	redacteur := e.createUser("md1@x.com", false)
	p := e.createProject("MD")
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)
	rec := e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/medias/does-not-exist", redacteur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMedia_UploadMissingFileField(t *testing.T) {
	e := newTestEnv(t)
	redacteur := e.createUser("md2@x.com", false)
	p := e.createProject("MD2")
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/medias", redacteur, map[string]string{"not": "multipart"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-multipart upload, got %d", rec.Code)
	}
}

func TestSchema_UpdateAndDeleteUnknown404(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("sd1@x.com", false)
	p := e.createProject("SD")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	def := trischema.Definition{Fields: []trischema.Field{{Key: "a", Type: trischema.Text, Cardinality: trischema.Simple}}}
	rec := e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/schemas/missing", concepteur, schemaRequest{Name: "X", Definition: def})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 updating unknown schema, got %d", rec.Code)
	}
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/schemas/missing", concepteur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting unknown schema, got %d", rec.Code)
	}
}

func TestFolder_MissingNameAndDeleteUnknown(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("fd1@x.com", false)
	p := e.createProject("FD")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/folders", concepteur, folderRequest{Name: ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/folders/does-not-exist", concepteur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteProject_MalformedBody(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("dp1@x.com", true)
	p := e.createProject("DP")
	rec := e.request(http.MethodDelete, "/api/v1/projects/"+p.ID, admin, map[string]int{"confirm_name": 1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTokenWebhook_DeleteUpdateUnknown404(t *testing.T) {
	e := newTestEnv(t)
	admin := e.createUser("tw1@x.com", true)
	p := e.createProject("TW")

	rec := e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/tokens/does-not-exist", admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	rec = e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/webhooks/does-not-exist", admin, webhookRequest{URL: "https://x.com", Secret: "s", Events: []string{"content.create"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/webhooks/does-not-exist", admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestContent_UnknownSchemaAndUnknownID(t *testing.T) {
	e := newTestEnv(t)
	redacteur := e.createUser("cu1@x.com", false)
	p := e.createProject("CU")
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/no-such-schema", redacteur, contentRequest{Data: map[string]any{"a": "b"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown schema, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/contents/no-such-schema/does-not-exist", redacteur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/contents/no-such-schema/does-not-exist", redacteur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestContent_MediaFieldAndCrossSchemaReference(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("cm1@x.com", false)
	redacteur := e.createUser("cm2@x.com", false)
	p := e.createProject("CM")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	setupSchema(t, e, p.ID, concepteur, "gallery_item", trischema.Definition{Fields: []trischema.Field{
		{Key: "cover", Type: trischema.Media, Cardinality: trischema.Simple, Required: true},
	}})
	setupSchema(t, e, p.ID, concepteur, "other", trischema.Definition{Fields: []trischema.Field{
		{Key: "name", Type: trischema.Text, Cardinality: trischema.Simple, Required: true},
	}})
	setupSchema(t, e, p.ID, concepteur, "linker", trischema.Definition{Fields: []trischema.Field{
		{Key: "target", Type: trischema.Reference, Cardinality: trischema.Simple, TargetSchema: "gallery_item"},
	}})

	// Missing media reference rejected.
	rec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/gallery_item", redacteur, contentRequest{Data: map[string]any{"cover": "no-such-media"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing media, got %d: %s", rec.Code, rec.Body.String())
	}

	// Upload a real media, then reference it successfully.
	up := uploadFile(t, e, "/api/v1/projects/"+p.ID+"/medias", redacteur, "img.png", []byte("data"))
	media := decodeBody[storage.Media](t, up)
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/gallery_item", redacteur, contentRequest{Data: map[string]any{"cover": media.ID}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// A content id that exists but belongs to a *different* schema than
	// targetSchema must be rejected as an invalid reference.
	otherRec := e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/other", redacteur, contentRequest{Data: map[string]any{"name": "X"}})
	other := decodeBody[contentResponse](t, otherRec)
	rec = e.request(http.MethodPost, "/api/v1/projects/"+p.ID+"/contents/linker", redacteur, contentRequest{Data: map[string]any{"target": other.ID}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-schema reference mismatch, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContent_UpdateUnknownID404(t *testing.T) {
	e := newTestEnv(t)
	concepteur := e.createUser("cx1@x.com", false)
	redacteur := e.createUser("cx2@x.com", false)
	p := e.createProject("CX")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)
	setupSchema(t, e, p.ID, concepteur, "thing", trischema.Definition{Fields: []trischema.Field{
		{Key: "name", Type: trischema.Text, Cardinality: trischema.Simple},
	}})
	rec := e.request(http.MethodPut, "/api/v1/projects/"+p.ID+"/contents/thing/does-not-exist", redacteur, contentRequest{Data: map[string]any{"name": "x"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
