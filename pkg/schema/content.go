package schema

import (
	"fmt"
	"net/url"
	"regexp"
	"time"
)

var (
	slugPattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// Hooks lets callers (pkg/api, backed by pkg/storage) plug in the
// cross-record checks that pkg/schema itself cannot perform in isolation:
// media/reference existence and slug uniqueness. Any nil hook skips that
// particular check (useful for unit-testing schema logic standalone).
type Hooks struct {
	// MediaExists reports whether a _medias.id exists in the project.
	MediaExists func(mediaID string) (bool, error)
	// ReferenceExists reports whether a content id exists within targetSchema.
	ReferenceExists func(targetSchema, contentID string) (bool, error)
	// SlugTaken reports whether `value` is already used as a Slug field
	// value within schemaSlug, excluding excludeContentID (used on update).
	SlugTaken func(schemaSlug, value, excludeContentID string) (bool, error)
}

// ValidateAndNormalize validates data (as decoded from JSON into
// map[string]any) against def and returns a normalized copy ready for
// persistence: RichText_HTML sanitized, missing optional Collection fields
// defaulted to an empty array, and only declared fields retained.
func ValidateAndNormalize(def *Definition, schemaSlug string, data map[string]any, excludeContentID string, hooks *Hooks) (map[string]any, error) {
	if hooks == nil {
		hooks = &Hooks{}
	}
	out := make(map[string]any, len(def.Fields))
	for _, field := range def.Fields {
		raw, present := data[field.Key]
		if !present || raw == nil {
			if field.Required {
				return nil, fmt.Errorf("schema: field %q is required", field.Key)
			}
			if field.Cardinality == Collection {
				out[field.Key] = []any{}
			}
			continue
		}

		if field.Cardinality == Collection {
			arr, ok := raw.([]any)
			if !ok {
				return nil, fmt.Errorf("schema: field %q must be an array (Collection)", field.Key)
			}
			normalized := make([]any, 0, len(arr))
			for i, item := range arr {
				v, err := validateScalar(field, item, schemaSlug, excludeContentID, hooks)
				if err != nil {
					return nil, fmt.Errorf("schema: field %q[%d]: %w", field.Key, i, err)
				}
				normalized = append(normalized, v)
			}
			out[field.Key] = normalized
			continue
		}

		if field.Type != JSONType {
			if _, isArray := raw.([]any); isArray {
				return nil, fmt.Errorf("schema: field %q is Simple, must not be an array", field.Key)
			}
		}
		v, err := validateScalar(field, raw, schemaSlug, excludeContentID, hooks)
		if err != nil {
			return nil, fmt.Errorf("schema: field %q: %w", field.Key, err)
		}
		out[field.Key] = v
	}
	return out, nil
}

func validateScalar(field Field, raw any, schemaSlug, excludeContentID string, hooks *Hooks) (any, error) {
	switch field.Type {
	case Text, RichTextMD:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		return s, nil

	case RichTextHTML:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		return SanitizeHTML(s), nil

	case Float:
		f, ok := raw.(float64)
		if !ok {
			return nil, fmt.Errorf("expected number")
		}
		return f, nil

	case Int:
		f, ok := raw.(float64)
		if !ok {
			return nil, fmt.Errorf("expected number")
		}
		if f != float64(int64(f)) {
			return nil, fmt.Errorf("expected integer value, got %v", f)
		}
		return f, nil

	case Boolean:
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected boolean")
		}
		return b, nil

	case Date:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return s, nil
		}
		if _, err := time.Parse(time.RFC3339, s); err == nil {
			return s, nil
		}
		return nil, fmt.Errorf("expected ISO 8601 date (YYYY-MM-DD) or RFC 3339 date-time, got %q", s)

	case Enum:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		for _, opt := range field.Options {
			if opt == s {
				return s, nil
			}
		}
		return nil, fmt.Errorf("value %q is not one of %v", s, field.Options)

	case Slug:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		if !slugPattern.MatchString(s) {
			return nil, fmt.Errorf("invalid slug format %q (expected lowercase alphanumeric with hyphens)", s)
		}
		if hooks.SlugTaken != nil {
			taken, err := hooks.SlugTaken(schemaSlug, s, excludeContentID)
			if err != nil {
				return nil, err
			}
			if taken {
				return nil, fmt.Errorf("slug %q is already in use", s)
			}
		}
		return s, nil

	case URL:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		u, err := url.ParseRequestURI(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("invalid URL %q", s)
		}
		return s, nil

	case Color:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		if !colorPattern.MatchString(s) {
			return nil, fmt.Errorf("invalid hex color %q (expected e.g. #1E90FF)", s)
		}
		return s, nil

	case JSONType:
		switch raw.(type) {
		case map[string]any, []any:
			return raw, nil
		default:
			return nil, fmt.Errorf("expected a JSON object or array")
		}

	case GeoPoint:
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object with lat/lng")
		}
		lat, latOK := obj["lat"].(float64)
		lng, lngOK := obj["lng"].(float64)
		if !latOK || !lngOK {
			return nil, fmt.Errorf("expected numeric lat and lng")
		}
		if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
			return nil, fmt.Errorf("lat/lng out of range")
		}
		return map[string]any{"lat": lat, "lng": lng}, nil

	case Media:
		s, ok := raw.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("expected non-empty media id")
		}
		if hooks.MediaExists != nil {
			exists, err := hooks.MediaExists(s)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf("media %q does not exist", s)
			}
		}
		return s, nil

	case Reference:
		s, ok := raw.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("expected non-empty content id")
		}
		if hooks.ReferenceExists != nil {
			exists, err := hooks.ReferenceExists(field.TargetSchema, s)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf("referenced content %q does not exist in schema %q", s, field.TargetSchema)
			}
		}
		return s, nil

	default:
		return nil, fmt.Errorf("unsupported field type %q", field.Type)
	}
}
