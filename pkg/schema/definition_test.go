package schema

import "testing"

func TestParseDefinition_Valid(t *testing.T) {
	raw := []byte(`{
		"fields": [
			{"key": "title", "label": "Title", "type": "Text", "cardinality": "Simple", "required": true},
			{"key": "gallery", "label": "Gallery", "type": "Media", "cardinality": "Collection"},
			{"key": "status", "label": "Status", "type": "Enum", "cardinality": "Simple", "options": ["draft", "review"]},
			{"key": "author", "label": "Author", "type": "Reference", "cardinality": "Simple", "targetSchema": "authors"}
		]
	}`)
	def, err := ParseDefinition(raw)
	if err != nil {
		t.Fatalf("expected valid definition, got %v", err)
	}
	if len(def.Fields) != 4 {
		t.Fatalf("expected 4 fields, got %d", len(def.Fields))
	}
	if f := def.FieldByKey("title"); f == nil || !f.Required {
		t.Fatalf("expected title field to be required")
	}
	if def.FieldByKey("missing") != nil {
		t.Fatal("expected nil for unknown key")
	}
}

func TestParseDefinition_InvalidJSON(t *testing.T) {
	if _, err := ParseDefinition([]byte(`not json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseDefinition_DuplicateKeys(t *testing.T) {
	raw := []byte(`{"fields": [
		{"key": "title", "type": "Text", "cardinality": "Simple"},
		{"key": "title", "type": "Text", "cardinality": "Simple"}
	]}`)
	if _, err := ParseDefinition(raw); err == nil {
		t.Fatal("expected error for duplicate field keys")
	}
}

func TestField_Validate(t *testing.T) {
	cases := []struct {
		name    string
		field   Field
		wantErr bool
	}{
		{"valid text", Field{Key: "a", Type: Text, Cardinality: Simple}, false},
		{"empty key", Field{Key: "", Type: Text, Cardinality: Simple}, true},
		{"invalid type", Field{Key: "a", Type: "Nope", Cardinality: Simple}, true},
		{"invalid cardinality", Field{Key: "a", Type: Text, Cardinality: "Nope"}, true},
		{"enum without options", Field{Key: "a", Type: Enum, Cardinality: Simple}, true},
		{"enum with options", Field{Key: "a", Type: Enum, Cardinality: Simple, Options: []string{"x"}}, false},
		{"reference without target", Field{Key: "a", Type: Reference, Cardinality: Simple}, true},
		{"reference with target", Field{Key: "a", Type: Reference, Cardinality: Simple, TargetSchema: "s"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.field.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestFieldType_Valid(t *testing.T) {
	if !Text.Valid() || !GeoPoint.Valid() {
		t.Fatal("expected known types to be valid")
	}
	if FieldType("Bogus").Valid() {
		t.Fatal("expected unknown type to be invalid")
	}
}

func TestCardinality_Valid(t *testing.T) {
	if !Simple.Valid() || !Collection.Valid() {
		t.Fatal("expected known cardinalities valid")
	}
	if Cardinality("Bogus").Valid() {
		t.Fatal("expected unknown cardinality invalid")
	}
}
