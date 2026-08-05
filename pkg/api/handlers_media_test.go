package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"tricms/pkg/storage"
)

func uploadFile(t *testing.T, e *testEnv, path string, u *storage.User, filename string, content []byte) *httptest.ResponseRecorder {
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

func TestMedia_UploadListDelete(t *testing.T) {
	e := newTestEnv(t)
	redacteur := e.createUser("m1@x.com", false)
	concepteur := e.createUser("m2@x.com", false)
	p := e.createProject("Gallery")
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	rec := uploadFile(t, e, "/api/v1/projects/"+p.ID+"/medias", redacteur, "logo.png", []byte("fake-png-bytes"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	media := decodeBody[storage.Media](t, rec)
	if media.Filename != "logo.png" || media.Size != int64(len("fake-png-bytes")) {
		t.Fatalf("unexpected media record: %+v", media)
	}

	rec = e.request(http.MethodGet, "/api/v1/projects/"+p.ID+"/medias", concepteur, nil)
	list := decodeBody[[]storage.Media](t, rec)
	if len(list) != 1 {
		t.Fatalf("expected 1 media, got %d", len(list))
	}

	rec = e.request(http.MethodDelete, "/api/v1/projects/"+p.ID+"/medias/"+media.ID, redacteur, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestMedia_OutsiderForbidden(t *testing.T) {
	e := newTestEnv(t)
	outsider := e.createUser("m3@x.com", false)
	p := e.createProject("Private")
	rec := uploadFile(t, e, "/api/v1/projects/"+p.ID+"/medias", outsider, "x.png", []byte("x"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
