// Package schema validates content-schema definitions and the JSON payloads
// stored against them (spec section 3): field types, Simple/Collection
// cardinality, and the extra invariants each type carries (Enum options,
// Reference target existence, Slug uniqueness/format, HTML sanitization…).
package schema

import "fmt"

// FieldType enumerates every supported field type (spec §3 table).
type FieldType string

const (
	Text         FieldType = "Text"
	RichTextMD   FieldType = "RichText_MD"
	RichTextHTML FieldType = "RichText_HTML"
	Float        FieldType = "Float"
	Int          FieldType = "Int"
	Date         FieldType = "Date"
	Media        FieldType = "Media"
	Boolean      FieldType = "Boolean"
	Enum         FieldType = "Enum"
	Reference    FieldType = "Reference"
	Slug         FieldType = "Slug"
	URL          FieldType = "URL"
	Color        FieldType = "Color"
	JSONType     FieldType = "JSON"
	GeoPoint     FieldType = "GeoPoint"
)

var validFieldTypes = map[FieldType]bool{
	Text: true, RichTextMD: true, RichTextHTML: true, Float: true, Int: true,
	Date: true, Media: true, Boolean: true, Enum: true, Reference: true,
	Slug: true, URL: true, Color: true, JSONType: true, GeoPoint: true,
}

func (t FieldType) Valid() bool { return validFieldTypes[t] }

// Cardinality controls whether a field's value in `_contents.data` is a
// single scalar ("Simple") or a JSON array of scalars ("Collection").
type Cardinality string

const (
	Simple     Cardinality = "Simple"
	Collection Cardinality = "Collection"
)

func (c Cardinality) Valid() bool {
	return c == Simple || c == Collection
}

// Field is one entry of a schema Definition (spec §3 JSON structure).
type Field struct {
	Key          string      `json:"key"`
	Label        string      `json:"label"`
	Type         FieldType   `json:"type"`
	Cardinality  Cardinality `json:"cardinality"`
	Placeholder  string      `json:"placeholder,omitempty"`
	Required     bool        `json:"required"`
	Options      []string    `json:"options,omitempty"`      // Enum only
	TargetSchema string      `json:"targetSchema,omitempty"` // Reference only
}

// Validate checks a single field's own invariants, independent of any content.
func (f Field) Validate() error {
	if f.Key == "" {
		return fmt.Errorf("schema: field key must not be empty")
	}
	if !f.Type.Valid() {
		return fmt.Errorf("schema: field %q has invalid type %q", f.Key, f.Type)
	}
	if !f.Cardinality.Valid() {
		return fmt.Errorf("schema: field %q has invalid cardinality %q", f.Key, f.Cardinality)
	}
	if f.Type == Enum && len(f.Options) == 0 {
		return fmt.Errorf("schema: field %q of type Enum requires non-empty options", f.Key)
	}
	if f.Type == Reference && f.TargetSchema == "" {
		return fmt.Errorf("schema: field %q of type Reference requires targetSchema", f.Key)
	}
	return nil
}
