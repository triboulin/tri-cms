package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tricms/pkg/storage"
)

// NewRouter builds the full HTTP router: the JSON REST API under /api/v1
// (both browser-session and API-token authenticated) plus, if templates
// were provided, the server-rendered HTMX admin UI.
func NewRouter(s *Server) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.authenticate)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/me", s.handleMe)

		r.Group(func(r chi.Router) {
			r.Use(s.requireIdentity)

			// Listing projects works for any authenticated identity (session
			// user or API token, every token being global-admin equivalent);
			// handleListProjects itself scopes the result per-identity.
			r.Get("/projects", s.handleListProjects)
			r.With(s.requireGlobalAdmin).Post("/projects", s.handleCreateProject)
			// Deliberately no DELETE /projects/{projectID} here: project
			// deletion is destructive and easy to trigger by mistake from a
			// script, so it's reachable only through the HTMX admin UI
			// (htmxAdminDeleteProject), which forces a re-typed-name
			// double-confirmation. See spec_sveltekit_tricms_cloudflare.md
			// §9 for the rationale.

			// API tokens: global scope, Administration-only (spec §1, §4.3).
			// Every token minted here is ADMIN-equivalent -- see
			// pkg/auth middleware and handlers_tokens.go.
			r.With(s.requireGlobalAdmin).Get("/tokens", s.handleListAPITokens)
			r.With(s.requireGlobalAdmin).Post("/tokens", s.handleCreateAPIToken)
			r.With(s.requireGlobalAdmin).Delete("/tokens/{tokenID}", s.handleDeleteAPIToken)

			r.With(s.requireGlobalAdmin).Get("/users", s.handleListUsers)
			r.With(s.requireGlobalAdmin).Post("/users", s.handleCreateUser)
			r.With(s.requireGlobalAdmin).Patch("/users/{userID}", s.handleUpdateUser)
			r.With(s.requireGlobalAdmin).Delete("/users/{userID}", s.handleDeleteUser)

			r.Route("/projects/{projectID}", func(r chi.Router) {
				r.Use(s.projectContext)

				// Project user management (spec §4.4): GESTIONNAIRE+.
				r.With(s.requireProjectRole(storage.RoleGestionnaire)).Get("/users", s.handleListProjectUsers)
				r.With(s.requireProjectRole(storage.RoleGestionnaire)).Post("/users", s.handleAssignProjectUser)
				r.With(s.requireProjectRole(storage.RoleGestionnaire)).Delete("/users/{userID}", s.handleRemoveProjectUser)

				// Conception (folders/schemas): read for all roles, write CONCEPTEUR+.
				r.With(s.requireProjectAccess).Get("/folders", s.handleListFolders)
				r.With(s.requireProjectRole(storage.RoleConcepteur)).Post("/folders", s.handleCreateFolder)
				r.With(s.requireProjectRole(storage.RoleConcepteur)).Delete("/folders/{folderID}", s.handleDeleteFolder)

				r.With(s.requireProjectAccess).Get("/schemas", s.handleListSchemas)
				r.With(s.requireProjectAccess).Get("/schemas/{schemaSlug}", s.handleGetSchema)
				r.With(s.requireProjectRole(storage.RoleConcepteur)).Post("/schemas", s.handleCreateSchema)
				r.With(s.requireProjectRole(storage.RoleConcepteur)).Put("/schemas/{schemaSlug}", s.handleUpdateSchema)
				r.With(s.requireProjectRole(storage.RoleConcepteur)).Delete("/schemas/{schemaSlug}", s.handleDeleteSchema)

				// Contents (spec §2.2 note: /api/v1/contents/{slug}): read for
				// all roles, write REDACTEUR+.
				r.With(s.requireProjectAccess).Get("/contents/{schemaSlug}", s.handleListContents)
				r.With(s.requireProjectAccess).Get("/contents/{schemaSlug}/{contentID}", s.handleGetContent)
				r.With(s.requireProjectRole(storage.RoleRedacteur)).Post("/contents/{schemaSlug}", s.handleCreateContent)
				r.With(s.requireProjectRole(storage.RoleRedacteur)).Put("/contents/{schemaSlug}/{contentID}", s.handleUpdateContent)
				r.With(s.requireProjectRole(storage.RoleRedacteur)).Delete("/contents/{schemaSlug}/{contentID}", s.handleDeleteContent)

				// Media: read for all roles, write REDACTEUR+.
				r.With(s.requireProjectAccess).Get("/medias", s.handleListMedias)
				r.With(s.requireProjectRole(storage.RoleRedacteur)).Post("/medias", s.handleUploadMedia)
				r.With(s.requireProjectRole(storage.RoleRedacteur)).Delete("/medias/{mediaID}", s.handleDeleteMedia)

				// Webhooks: global-admin only (spec §1, §4.2), still project-scoped.
				r.With(s.requireGlobalAdmin).Get("/webhooks", s.handleListWebhooks)
				r.With(s.requireGlobalAdmin).Post("/webhooks", s.handleCreateWebhook)
				r.With(s.requireGlobalAdmin).Put("/webhooks/{webhookID}", s.handleUpdateWebhook)
				r.With(s.requireGlobalAdmin).Delete("/webhooks/{webhookID}", s.handleDeleteWebhook)

				// Audit log: global-admin only, scoped to this project (not
				// a cross-project firehose).
				r.With(s.requireGlobalAdmin).Get("/logs", s.handleListLogs)
			})
		})
	})

	if s.Templates != nil {
		mountHTMXRoutes(r, s)
		// Unmatched routes get chi's default plain-text 404 for the JSON API
		// (API clients expect a bare 404, not HTML), but a page styled like
		// the rest of the app for everything else -- a stray/old bookmarked
		// admin URL shouldn't dump the visitor onto a bare browser error page.
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			s.htmxNotFoundPage(w, r)
		})
	}

	return r
}
