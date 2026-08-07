package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// authenticate resolves the caller's identity from either a session cookie
// (browser/HTMX) or a `Authorization: Bearer <token>` header (machine API
// clients), populating the request context accordingly. It never rejects a
// request by itself -- downstream `require*` middleware does that -- so
// public routes (login, health) can share the same chain.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			plaintext := strings.TrimPrefix(h, "Bearer ")
			hash := auth.HashAPIToken(plaintext)
			tok, err := s.System.GetAPITokenByHash(ctx, hash)
			if err == nil {
				ctx = withAPIToken(ctx, tok)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		if cookie, err := r.Cookie(s.SessionCookieName); err == nil {
			claims, err := s.Issuer.Parse(cookie.Value)
			if err == nil {
				user, err := s.System.GetUserByID(ctx, claims.UserID)
				if err == nil {
					ctx = withUser(ctx, user)
				}
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuth rejects requests without a resolved browser-session user.
// Use on routes that only make sense for a human operator (never for
// machine API-token clients).
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireIdentity rejects requests with neither a browser-session user nor
// a valid API token. It's the outer gate for the whole authenticated API
// surface; individual routes still enforce the *specific* identity they
// need (requireAuth for human-only actions, requireProjectRole for
// project-scoped ones, which itself accepts a project-matched API token).
func (s *Server) requireIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil && APITokenFromContext(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireGlobalAdmin rejects any caller who isn't global-admin-equivalent:
// a session user with IsGlobalAdmin, or any API token (every token is
// global-admin equivalent by design -- see ActorIsGlobalAdmin, specs.md
// §1). The one deliberate exception is project deletion, which has no API
// route at all (see router.go) regardless of this check.
func (s *Server) requireGlobalAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil && APITokenFromContext(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !ActorIsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "forbidden: global admin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// projectContext loads the {projectID} path param into context, along with
// the caller's project role (if any). It does not itself reject the
// request; use requireProjectRole/requireProjectAccess afterwards.
func (s *Server) projectContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		project, err := s.System.GetProject(r.Context(), projectID)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		ctx := withProject(r.Context(), project)

		if user := UserFromContext(ctx); user != nil && !user.IsGlobalAdmin {
			pp, err := s.System.GetProjectPermission(ctx, user.ID, project.ID)
			switch {
			case err == nil:
				role := pp.Role
				ctx = withProjectRole(ctx, &role)
			case errors.Is(err, storage.ErrNotFound):
				// no role: leave nil, requireProjectRole will reject.
			default:
				writeStorageError(w, err)
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireProjectRole rejects the request unless the caller is a global
// admin, holds at least `min` role on the in-scope project, or presents an
// API token: every token is global-admin equivalent (specs.md §1), so a
// token satisfies any `min` on any project, not just a matching one.
func (s *Server) requireProjectRole(min storage.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tok := APITokenFromContext(r.Context()); tok != nil {
				next.ServeHTTP(w, r)
				return
			}

			user := UserFromContext(r.Context())
			if user == nil {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if user.IsGlobalAdmin {
				next.ServeHTTP(w, r)
				return
			}
			role := ProjectRoleFromContext(r.Context())
			if role == nil || !auth.HasRole(*role, min) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireProjectAccess allows any caller with *some* relationship to the
// project (any role, global admin, or a scoped API token) through -- used
// for read-only endpoints available to all project roles.
func (s *Server) requireProjectAccess(next http.Handler) http.Handler {
	return s.requireProjectRole(storage.RoleRedacteur)(next)
}
