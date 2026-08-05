// Package web embeds triCMS's server-rendered HTMX templates and static
// assets (CSS/JS) directly into the compiled binary, so triCMS ships and
// deploys as a single self-contained executable.
package web

import "embed"

//go:embed templates static
var FS embed.FS
