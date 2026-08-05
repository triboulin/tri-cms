package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"tricms/pkg/storage"
)

// fieldFormValues builds the fields[N][prop]=value form entries the
// field-builder JS/template produces for one schema field row.
func fieldFormValues(idx int, key, label, typ, cardinality string, required bool, options, target, placeholder string) url.Values {
	v := url.Values{}
	p := func(name, value string) {
		if value != "" {
			v.Set("fields["+itoa(idx)+"]["+name+"]", value)
		}
	}
	p("key", key)
	p("label", label)
	p("type", typ)
	p("cardinality", cardinality)
	if required {
		v.Set("fields["+itoa(idx)+"][required]", "true")
	}
	p("options", options)
	p("targetSchema", target)
	p("placeholder", placeholder)
	return v
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

func mergeValues(vs ...url.Values) url.Values {
	out := url.Values{}
	for _, v := range vs {
		for k, vals := range v {
			out[k] = append(out[k], vals...)
		}
	}
	return out
}

func TestHTMX_Conception_SchemaCreateEditDelete(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("cons1@x.com", false)
	redacteur := e.createUser("cons2@x.com", false)
	p := e.createProject("ConceptionSchemas")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)
	e.setRole(redacteur.ID, p.ID, storage.RoleRedacteur)

	base := url.Values{"slug": {"article"}, "name": {"Article"}}
	fields := fieldFormValues(0, "title", "Titre", "Text", "Simple", true, "", "", "Titre de l'article")
	form := mergeValues(base, fields)

	// REDACTEUR forbidden from creating schemas.
	rec := e.postForm("/projects/"+p.ID+"/schemas/create", redacteur, form)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	rec = e.postForm("/projects/"+p.ID+"/schemas/create", concepteur, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Header().Get("Location"), "flash_kind=error") {
		t.Fatalf("unexpected error flash: %q", rec.Header().Get("Location"))
	}

	db, err := e.server.Manager.OpenProjectStorage(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sc, err := db.GetSchema(bgCtx(), "article")
	if err != nil {
		t.Fatalf("expected schema created: %v", err)
	}
	if !strings.Contains(sc.Definition, `"title"`) {
		t.Fatalf("expected field 'title' in definition, got %s", sc.Definition)
	}

	// Edit page renders with the existing field prefilled.
	editPage := e.getHTML("/projects/"+p.ID+"/schemas/article/edit", concepteur)
	if editPage.Code != http.StatusOK || !strings.Contains(editPage.Body.String(), `value="title"`) {
		t.Fatalf("expected edit page prefilled with existing field, got %d: %s", editPage.Code, editPage.Body.String())
	}

	// Update: add a second field.
	updateForm := mergeValues(
		url.Values{"name": {"News Article"}},
		fieldFormValues(0, "title", "Titre", "Text", "Simple", true, "", "", ""),
		fieldFormValues(1, "body", "Corps", "RichText_MD", "Simple", false, "", "", ""),
	)
	rec = e.postForm("/projects/"+p.ID+"/schemas/article/update", concepteur, updateForm)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	sc, _ = db.GetSchema(bgCtx(), "article")
	if sc.Name != "News Article" || !strings.Contains(sc.Definition, `"body"`) {
		t.Fatalf("expected schema updated with body field, got %+v", sc)
	}

	rec = e.postForm("/projects/"+p.ID+"/schemas/article/delete", concepteur, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if _, err := db.GetSchema(bgCtx(), "article"); err == nil {
		t.Fatal("expected schema deleted")
	}
}

func TestHTMX_Conception_InvalidDefinitionRejected(t *testing.T) {
	e := newHTMXTestEnv(t)
	concepteur := e.createUser("cons3@x.com", false)
	p := e.createProject("ConceptionInvalid")
	e.setRole(concepteur.ID, p.ID, storage.RoleConcepteur)

	// Enum field without options is invalid per pkg/schema.Field.Validate.
	form := mergeValues(
		url.Values{"slug": {"bad"}, "name": {"Bad"}},
		fieldFormValues(0, "status", "Status", "Enum", "Simple", false, "", "", ""),
	)
	rec := e.postForm("/projects/"+p.ID+"/schemas/create", concepteur, form)
	// The form is re-rendered (200) with the error and the submitted values
	// intact, rather than redirecting to a blank form and losing them.
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Définition invalide") {
		t.Fatalf("expected re-rendered form with inline error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `value="bad"`) || !strings.Contains(rec.Body.String(), `value="Bad"`) {
		t.Fatalf("expected submitted slug/name preserved in the re-rendered form, got %s", rec.Body.String())
	}
	// The field row itself (key "status") must also survive the re-render,
	// not just the slug/name -- this is the field-builder data that used to
	// be silently discarded.
	if !strings.Contains(rec.Body.String(), `value="status"`) {
		t.Fatalf("expected submitted field row preserved in the re-rendered form, got %s", rec.Body.String())
	}
}
