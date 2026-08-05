package schema

import (
	"encoding/json"
	"fmt"
)

// Definition is the parsed contents of `_schemas.definition`.
type Definition struct {
	Fields []Field `json:"fields"`
}

// ParseDefinition unmarshals and validates a schema definition's raw JSON.
func ParseDefinition(raw []byte) (*Definition, error) {
	var def Definition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("schema: invalid definition JSON: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return &def, nil
}

// Validate checks every field and rejects duplicate keys.
func (d *Definition) Validate() error {
	seen := make(map[string]bool, len(d.Fields))
	for _, f := range d.Fields {
		if err := f.Validate(); err != nil {
			return err
		}
		if seen[f.Key] {
			return fmt.Errorf("schema: duplicate field key %q", f.Key)
		}
		seen[f.Key] = true
	}
	return nil
}

// FieldByKey returns the field definition for key, or nil if absent.
func (d *Definition) FieldByKey(key string) *Field {
	for i := range d.Fields {
		if d.Fields[i].Key == key {
			return &d.Fields[i]
		}
	}
	return nil
}
