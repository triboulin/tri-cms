package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"tricms/pkg/storage"
)

// uploadImageForm uploads a media file with an explicit image/png Content-
// Type on the multipart part -- mime/multipart's CreateFormFile always sends
// application/octet-stream regardless of filename, which would make every
// test upload look like a non-image to the media picker's IsImage check.
func uploadImageForm(t *testing.T, e *testEnv, path string, u *storage.User, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	header.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(header)
	if err != nil {
		t.Fatalf("create form part: %v", err)
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

func createSchemaViaHTMX(t *testing.T, e *testEnv, projectID string, concepteur *storage.User, slug string, fields url.Values) {
	t.Helper()
	form := mergeValues(url.Values{"slug": {slug}, "name": {slug}}, fields)
	rec := e.postForm("/projects/"+projectID+"/schemas/create", concepteur, form)
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("setup schema %q failed: %d %q %s", slug, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}

func TestHTMX_ContentGrid_FullLifecycle(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("cg1@x.com", false)
	redacteur := e.createUser("cg2@x.com", false)
	p := e.createProject("ContentGrid")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	createSchemaViaHTMX(t, e, p.ID, concepteur, "post", mergeValues(
		fieldFormValues(0, "title", "Titre", "Text", "Simple", true, "", "", ""),
		fieldFormValues(1, "tags", "Tags", "Text", "Collection", false, "", "", ""),
	))

	// List page (empty) shows the "add" CTA.
	listPage := e.getHTML("/projects/"+p.ID+"/schemas/post/contents", redacteur)
	if listPage.Code != http.StatusOK || !strings.Contains(listPage.Body.String(), "Ajouter un contenu") {
		t.Fatalf("expected empty content list page, got %d", listPage.Code)
	}

	// Create.
	rec := e.postForm("/projects/"+p.ID+"/schemas/post/contents/create", redacteur, url.Values{
		"title": {"Hello World"}, "tags": {"go, cms"}, "status": {"draft"},
	})
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected redirect success, got %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	db, err := e.server.Manager.OpenProjectStorage(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, err := db.ListContents(bgCtx(), "post")
	if err != nil || len(items) != 1 {
		t.Fatalf("expected 1 content: %v (%d)", err, len(items))
	}
	contentID := items[0].ID

	// List page now shows the row.
	listPage = e.getHTML("/projects/"+p.ID+"/schemas/post/contents", redacteur)
	if !strings.Contains(listPage.Body.String(), "Hello World") || !strings.Contains(listPage.Body.String(), "go, cms") {
		t.Fatalf("expected content row rendered, got %s", listPage.Body.String())
	}

	// The row must be clickable (edit/delete URLs on the <tr>) and no
	// per-row action icons/forms should remain in the markup -- editing and
	// deleting both now happen through the row-click modal.
	listBody := listPage.Body.String()
	if !strings.Contains(listBody, `data-edit-url="/projects/`+p.ID+`/schemas/post/contents/`+contentID+`/edit"`) {
		t.Fatalf("expected row data-edit-url, got %s", listBody)
	}
	if !strings.Contains(listBody, `data-delete-url="/projects/`+p.ID+`/schemas/post/contents/`+contentID+`/delete"`) {
		t.Fatalf("expected row data-delete-url, got %s", listBody)
	}
	if strings.Contains(listBody, `class="tri-actions"`) {
		t.Fatalf("expected no more per-row action column, got %s", listBody)
	}
	if !strings.Contains(listBody, `id="content-edit-modal"`) {
		t.Fatalf("expected row-edit modal skeleton present, got %s", listBody)
	}

	// Edit form prefilled.
	editPage := e.getHTML("/projects/"+p.ID+"/schemas/post/contents/"+contentID+"/edit", redacteur)
	if editPage.Code != http.StatusOK || !strings.Contains(editPage.Body.String(), `value="Hello World"`) {
		t.Fatalf("expected prefilled edit form, got %d: %s", editPage.Code, editPage.Body.String())
	}

	// Update.
	rec = e.postForm("/projects/"+p.ID+"/schemas/post/contents/"+contentID+"/update", redacteur, url.Values{
		"title": {"Hello World Updated"}, "tags": {""}, "status": {"published"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, _ := db.GetContent(bgCtx(), contentID)
	if updated.Status != storage.StatusPublished || !strings.Contains(updated.Data, "Hello World Updated") {
		t.Fatalf("expected update applied: %+v", updated)
	}

	// Toggle status back to draft.
	rec = e.postForm("/projects/"+p.ID+"/schemas/post/contents/"+contentID+"/toggle-status", redacteur, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	toggled, _ := db.GetContent(bgCtx(), contentID)
	if toggled.Status != storage.StatusDraft {
		t.Fatalf("expected toggled back to draft, got %s", toggled.Status)
	}

	// Delete.
	rec = e.postForm("/projects/"+p.ID+"/schemas/post/contents/"+contentID+"/delete", redacteur, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if _, err := db.GetContent(bgCtx(), contentID); err == nil {
		t.Fatal("expected content deleted")
	}
}

func TestHTMX_ContentGrid_RequiredFieldValidation(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("cg3@x.com", false)
	p := e.createProject("ContentValidation")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	createSchemaViaHTMX(t, e, p.ID, concepteur, "item",
		fieldFormValues(0, "name", "Nom", "Text", "Simple", true, "", "", ""))

	rec := e.postForm("/projects/"+p.ID+"/schemas/item/contents/create", concepteur, url.Values{"name": {""}})
	// The form is re-rendered (200) with the error instead of redirecting to
	// a blank form and losing whatever else the user had filled in.
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Validation") {
		t.Fatalf("expected re-rendered form with inline validation error, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHTMX_ContentGrid_ValuesPreservedOnValidationError guards against the
// PRG data-loss bug identified in the UX audit: a validation error used to
// redirect to a blank form, discarding every field the user had typed. The
// form must now be re-rendered with everything intact.
func TestHTMX_ContentGrid_ValuesPreservedOnValidationError(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("cg5@x.com", false)
	p := e.createProject("ContentPreserve")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	createSchemaViaHTMX(t, e, p.ID, concepteur, "post", mergeValues(
		fieldFormValues(0, "title", "Titre", "Text", "Simple", true, "", "", ""),
		fieldFormValues(1, "subtitle", "Sous-titre", "Text", "Simple", false, "", "", ""),
	))

	// "title" is required and left empty; "subtitle" is filled in and must
	// survive the re-render even though the submission as a whole fails.
	rec := e.postForm("/projects/"+p.ID+"/schemas/post/contents/create", concepteur, url.Values{
		"title": {""}, "subtitle": {"Kept across the error"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected re-rendered form, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Kept across the error") {
		t.Fatalf("expected untouched field value preserved in re-rendered form, got %s", rec.Body.String())
	}

	// Now create it successfully, then verify an edit-time validation error
	// also preserves the (changed) field values instead of reverting to the
	// stored ones.
	rec = e.postForm("/projects/"+p.ID+"/schemas/post/contents/create", concepteur, url.Values{
		"title": {"Hello"}, "subtitle": {"Original"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect on success, got %d: %s", rec.Code, rec.Body.String())
	}
	db, err := e.server.Manager.OpenProjectStorage(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, _ := db.ListContents(bgCtx(), "post")
	contentID := items[0].ID

	rec = e.postForm("/projects/"+p.ID+"/schemas/post/contents/"+contentID+"/update", concepteur, url.Values{
		"title": {""}, "subtitle": {"Edited but not saved"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected re-rendered edit form, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Edited but not saved") {
		t.Fatalf("expected in-progress edit preserved in re-rendered form, got %s", rec.Body.String())
	}
}

func TestHTMX_ContentGrid_ReferenceGuardWithForce(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("cg4@x.com", false)
	p := e.createProject("ContentReference")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	createSchemaViaHTMX(t, e, p.ID, concepteur, "author",
		fieldFormValues(0, "name", "Nom", "Text", "Simple", true, "", "", ""))
	createSchemaViaHTMX(t, e, p.ID, concepteur, "post",
		fieldFormValues(0, "author", "Auteur", "Reference", "Simple", false, "", "author", ""))

	rec := e.postForm("/projects/"+p.ID+"/schemas/author/contents/create", concepteur, url.Values{"name": {"Ada"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	db, _ := e.server.Manager.OpenProjectStorage(p.ID)
	defer db.Close()
	authors, _ := db.ListContents(bgCtx(), "author")
	authorID := authors[0].ID

	// Reference-select form field is named after the field key.
	rec = e.postForm("/projects/"+p.ID+"/schemas/post/contents/create", concepteur, url.Values{"author": {authorID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	// Deleting the referenced author without force is refused.
	rec = e.postForm("/projects/"+p.ID+"/schemas/author/contents/"+authorID+"/delete", concepteur, url.Values{})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected error flash (referenced), got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := db.GetContent(bgCtx(), authorID); err != nil {
		t.Fatal("author should not have been deleted yet")
	}

	// With force=true it proceeds.
	rec = e.postForm("/projects/"+p.ID+"/schemas/author/contents/"+authorID+"/delete", concepteur, url.Values{"force": {"true"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if _, err := db.GetContent(bgCtx(), authorID); err == nil {
		t.Fatal("expected author deleted with force")
	}
}

// TestHTMX_ContentForm_MediaPicker guards the thumbnail-grid media picker
// (replacing a bare <select> of media IDs): the form must render without a
// template error for both a single Media field and a Collection-of-Media
// field, must expose each uploaded media as a picker item, and a previously
// selected media must reappear as a preview chip when re-editing.
func TestHTMX_ContentForm_MediaPicker(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("cgmp1@x.com", false)
	p := e.createProject("ContentMediaPicker")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	createSchemaViaHTMX(t, e, p.ID, concepteur, "gallery", mergeValues(
		fieldFormValues(0, "cover", "Couverture", "Media", "Simple", false, "", "", ""),
		fieldFormValues(1, "extras", "Extras", "Media", "Collection", false, "", "", ""),
	))

	upload := uploadImageForm(t, e, "/projects/"+p.ID+"/medias/upload", concepteur, "pic.png", []byte("fake-png-bytes"))
	if upload.Code != http.StatusSeeOther {
		t.Fatalf("media upload failed: %d %s", upload.Code, upload.Body.String())
	}
	db, err := e.server.Manager.OpenProjectStorage(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	medias, err := db.ListMedias(bgCtx())
	if err != nil || len(medias) != 1 {
		t.Fatalf("expected 1 media: %v %+v", err, medias)
	}
	mediaID := medias[0].ID

	// The "new content" form must render the picker (not crash / not fall
	// back to a bare select) and list the uploaded media as a pickable item.
	newForm := e.getHTML("/projects/"+p.ID+"/schemas/gallery/contents/new", concepteur)
	if newForm.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", newForm.Code, newForm.Body.String())
	}
	body := newForm.Body.String()
	for _, want := range []string{
		`data-key="cover"`, `data-multi="false"`,
		`data-key="extras"`, `data-multi="true"`,
		`data-label="pic.png"`, `<img src="/projects/` + p.ID + `/medias/` + mediaID + `/file"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected form to contain %q, got:\n%s", want, body)
		}
	}

	// Create content selecting that media for both fields (hidden inputs
	// produced by the picker just submit as normal form values).
	rec := e.postForm("/projects/"+p.ID+"/schemas/gallery/contents/create", concepteur, url.Values{
		"cover": {mediaID}, "extras": {mediaID}, "status": {"draft"},
	})
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("expected redirect success, got %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	contents, err := db.ListContents(bgCtx(), "gallery")
	if err != nil || len(contents) != 1 {
		t.Fatalf("expected 1 content: %v %+v", err, contents)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(contents[0].Data), &data); err != nil {
		t.Fatal(err)
	}
	if data["cover"] != mediaID {
		t.Fatalf("expected cover=%q, got %v", mediaID, data["cover"])
	}
	extras, ok := data["extras"].([]any)
	if !ok || len(extras) != 1 || extras[0] != mediaID {
		t.Fatalf("expected extras=[%q], got %v", mediaID, data["extras"])
	}

	// Re-editing must show the previously selected media as a preview chip.
	editForm := e.getHTML("/projects/"+p.ID+"/schemas/gallery/contents/"+contents[0].ID+"/edit", concepteur)
	if editForm.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", editForm.Code, editForm.Body.String())
	}
	editBody := editForm.Body.String()
	if !strings.Contains(editBody, `class="tri-media-picker-chip"`) || !strings.Contains(editBody, "pic.png") {
		t.Fatalf("expected selected media chip in edit form, got:\n%s", editBody)
	}
}
