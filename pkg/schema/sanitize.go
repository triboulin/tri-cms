package schema

import "github.com/microcosm-cc/bluemonday"

// htmlPolicy is a permissive-but-safe policy for RichText_HTML fields:
// standard formatting/structure tags allowed, but scripts, inline event
// handlers, and javascript: URLs are always stripped (XSS prevention,
// spec §3 security note).
var htmlPolicy = bluemonday.UGCPolicy()

// SanitizeHTML strips any unsafe markup from user-supplied HTML before it
// is persisted or rendered. Must be called on every RichText_HTML value.
func SanitizeHTML(raw string) string {
	return htmlPolicy.Sanitize(raw)
}
