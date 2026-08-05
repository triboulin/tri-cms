package schema

import (
	"strings"
	"testing"
)

func mustDef(t *testing.T, fields ...Field) *Definition {
	t.Helper()
	def := &Definition{Fields: fields}
	if err := def.Validate(); err != nil {
		t.Fatalf("invalid test definition: %v", err)
	}
	return def
}

func TestValidateAndNormalize_SimpleFields(t *testing.T) {
	def := mustDef(t,
		Field{Key: "title", Type: Text, Cardinality: Simple, Required: true},
		Field{Key: "views", Type: Int, Cardinality: Simple},
		Field{Key: "rating", Type: Float, Cardinality: Simple},
		Field{Key: "active", Type: Boolean, Cardinality: Simple},
	)
	data := map[string]any{
		"title":  "Hello",
		"views":  float64(42),
		"rating": 4.5,
		"active": true,
	}
	out, err := ValidateAndNormalize(def, "post", data, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["title"] != "Hello" || out["views"] != float64(42) || out["active"] != true {
		t.Fatalf("unexpected normalized output: %+v", out)
	}
}

func TestValidateAndNormalize_RequiredMissing(t *testing.T) {
	def := mustDef(t, Field{Key: "title", Type: Text, Cardinality: Simple, Required: true})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{}, "", nil); err == nil {
		t.Fatal("expected error for missing required field")
	}
}

func TestValidateAndNormalize_CollectionDefaultsEmpty(t *testing.T) {
	def := mustDef(t, Field{Key: "tags", Type: Text, Cardinality: Collection})
	out, err := ValidateAndNormalize(def, "post", map[string]any{}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := out["tags"].([]any)
	if !ok || len(arr) != 0 {
		t.Fatalf("expected empty array default, got %+v", out["tags"])
	}
}

func TestValidateAndNormalize_CollectionValues(t *testing.T) {
	def := mustDef(t, Field{Key: "specs", Type: Float, Cardinality: Collection})
	out, err := ValidateAndNormalize(def, "post", map[string]any{"specs": []any{12.5, 8.2}}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr := out["specs"].([]any)
	if len(arr) != 2 || arr[0] != 12.5 {
		t.Fatalf("unexpected collection values: %+v", arr)
	}
}

func TestValidateAndNormalize_SimpleRejectsArray(t *testing.T) {
	def := mustDef(t, Field{Key: "title", Type: Text, Cardinality: Simple})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"title": []any{"a", "b"}}, "", nil); err == nil {
		t.Fatal("expected error: Simple field must not be an array")
	}
}

func TestValidateAndNormalize_CollectionRejectsNonArray(t *testing.T) {
	def := mustDef(t, Field{Key: "tags", Type: Text, Cardinality: Collection})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"tags": "not-an-array"}, "", nil); err == nil {
		t.Fatal("expected error: Collection field must be an array")
	}
}

func TestValidateAndNormalize_RichTextHTMLSanitized(t *testing.T) {
	def := mustDef(t, Field{Key: "body", Type: RichTextHTML, Cardinality: Simple})
	out, err := ValidateAndNormalize(def, "post", map[string]any{
		"body": `<p>Hello</p><script>alert(1)</script>`,
	}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := out["body"].(string)
	if strings.Contains(body, "<script>") {
		t.Fatalf("expected script tag to be stripped, got %q", body)
	}
	if !strings.Contains(body, "<p>Hello</p>") {
		t.Fatalf("expected safe markup preserved, got %q", body)
	}
}

func TestValidateAndNormalize_IntRejectsFraction(t *testing.T) {
	def := mustDef(t, Field{Key: "n", Type: Int, Cardinality: Simple})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"n": 3.14}, "", nil); err == nil {
		t.Fatal("expected error for non-integer value")
	}
}

func TestValidateAndNormalize_Date(t *testing.T) {
	def := mustDef(t, Field{Key: "d", Type: Date, Cardinality: Simple})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"d": "2026-08-05"}, "", nil); err != nil {
		t.Fatalf("expected valid date-only, got %v", err)
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"d": "2026-08-05T10:00:00Z"}, "", nil); err != nil {
		t.Fatalf("expected valid RFC3339, got %v", err)
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"d": "not-a-date"}, "", nil); err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestValidateAndNormalize_Enum(t *testing.T) {
	def := mustDef(t, Field{Key: "status", Type: Enum, Cardinality: Simple, Options: []string{"draft", "review", "approved"}})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"status": "review"}, "", nil); err != nil {
		t.Fatalf("expected valid enum value, got %v", err)
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"status": "bogus"}, "", nil); err == nil {
		t.Fatal("expected error for value outside options")
	}
}

func TestValidateAndNormalize_Slug(t *testing.T) {
	def := mustDef(t, Field{Key: "slug", Type: Slug, Cardinality: Simple})

	if _, err := ValidateAndNormalize(def, "post", map[string]any{"slug": "hello-world-42"}, "", nil); err != nil {
		t.Fatalf("expected valid slug, got %v", err)
	}
	badCases := []string{"Hello World", "hello_world", "héllo", "-leading", "trailing-"}
	for _, s := range badCases {
		if _, err := ValidateAndNormalize(def, "post", map[string]any{"slug": s}, "", nil); err == nil {
			t.Fatalf("expected slug format error for %q", s)
		}
	}

	hooks := &Hooks{SlugTaken: func(schemaSlug, value, exclude string) (bool, error) {
		return value == "taken", nil
	}}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"slug": "taken"}, "", hooks); err == nil {
		t.Fatal("expected uniqueness violation error")
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"slug": "free"}, "", hooks); err != nil {
		t.Fatalf("expected free slug to pass, got %v", err)
	}
}

func TestValidateAndNormalize_URL(t *testing.T) {
	def := mustDef(t, Field{Key: "link", Type: URL, Cardinality: Simple})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"link": "https://example.com/page"}, "", nil); err != nil {
		t.Fatalf("expected valid url, got %v", err)
	}
	badURLs := []string{"not a url", "ftp://example.com", "javascript:alert(1)"}
	for _, u := range badURLs {
		if _, err := ValidateAndNormalize(def, "post", map[string]any{"link": u}, "", nil); err == nil {
			t.Fatalf("expected error for invalid url %q", u)
		}
	}
}

func TestValidateAndNormalize_Color(t *testing.T) {
	def := mustDef(t, Field{Key: "c", Type: Color, Cardinality: Simple})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"c": "#1E90FF"}, "", nil); err != nil {
		t.Fatalf("expected valid color, got %v", err)
	}
	for _, bad := range []string{"1E90FF", "#ZZZZZZ", "#FFF"} {
		if _, err := ValidateAndNormalize(def, "post", map[string]any{"c": bad}, "", nil); err == nil {
			t.Fatalf("expected error for invalid color %q", bad)
		}
	}
}

func TestValidateAndNormalize_JSON(t *testing.T) {
	def := mustDef(t, Field{Key: "blob", Type: JSONType, Cardinality: Simple})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"blob": map[string]any{"a": 1.0}}, "", nil); err != nil {
		t.Fatalf("expected object accepted, got %v", err)
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"blob": []any{1.0, 2.0}}, "", nil); err != nil {
		t.Fatalf("expected array accepted, got %v", err)
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"blob": "scalar"}, "", nil); err == nil {
		t.Fatal("expected error for scalar JSON value")
	}
}

func TestValidateAndNormalize_GeoPoint(t *testing.T) {
	def := mustDef(t, Field{Key: "loc", Type: GeoPoint, Cardinality: Simple})
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"loc": map[string]any{"lat": 48.85, "lng": 2.35}}, "", nil); err != nil {
		t.Fatalf("expected valid geopoint, got %v", err)
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"loc": map[string]any{"lat": 999.0, "lng": 2.35}}, "", nil); err == nil {
		t.Fatal("expected error for out-of-range lat")
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"loc": "not-an-object"}, "", nil); err == nil {
		t.Fatal("expected error for non-object geopoint")
	}
}

func TestValidateAndNormalize_MediaAndReferenceHooks(t *testing.T) {
	def := mustDef(t,
		Field{Key: "cover", Type: Media, Cardinality: Simple},
		Field{Key: "author", Type: Reference, Cardinality: Simple, TargetSchema: "authors"},
	)
	hooks := &Hooks{
		MediaExists: func(id string) (bool, error) { return id == "media-1", nil },
		ReferenceExists: func(targetSchema, id string) (bool, error) {
			return targetSchema == "authors" && id == "author-1", nil
		},
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"cover": "media-1", "author": "author-1"}, "", hooks); err != nil {
		t.Fatalf("expected valid refs, got %v", err)
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"cover": "missing", "author": "author-1"}, "", hooks); err == nil {
		t.Fatal("expected error for missing media")
	}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"cover": "media-1", "author": "missing"}, "", hooks); err == nil {
		t.Fatal("expected error for missing reference")
	}
	// Without hooks, existence is not checked (schema-only validation).
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"cover": "whatever", "author": "whatever"}, "", nil); err != nil {
		t.Fatalf("expected no existence check without hooks, got %v", err)
	}
}

func TestValidateAndNormalize_UnsupportedTypeRejected(t *testing.T) {
	def := &Definition{Fields: []Field{{Key: "x", Type: FieldType("Weird"), Cardinality: Simple}}}
	if _, err := ValidateAndNormalize(def, "post", map[string]any{"x": "y"}, "", nil); err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestSanitizeHTML(t *testing.T) {
	out := SanitizeHTML(`<img src=x onerror=alert(1)><b>ok</b>`)
	if strings.Contains(out, "onerror") {
		t.Fatalf("expected event handler stripped, got %q", out)
	}
	if !strings.Contains(out, "<b>ok</b>") {
		t.Fatalf("expected safe tag preserved, got %q", out)
	}
}
