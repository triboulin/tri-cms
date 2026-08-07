// Package api exposes the HTTP surface of triCMS: the JSON REST API under
// /api/v1 and the server-rendered HTMX admin UI, both enforcing the RBAC
// matrix from pkg/auth on top of the multi-tenant storage in pkg/storage.
package api

import (
	"context"

	"tricms/pkg/storage"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxProject
	ctxProjectRole // *storage.Role, nil if the user has no project-scoped role
	ctxAPIToken
)

func withUser(ctx context.Context, u *storage.User) context.Context {
	return context.WithValue(ctx, ctxUser, u)
}

// UserFromContext returns the authenticated user, or nil if unauthenticated.
func UserFromContext(ctx context.Context) *storage.User {
	u, _ := ctx.Value(ctxUser).(*storage.User)
	return u
}

func withProject(ctx context.Context, p *storage.Project) context.Context {
	return context.WithValue(ctx, ctxProject, p)
}

// ProjectFromContext returns the project currently in scope, if any.
func ProjectFromContext(ctx context.Context) *storage.Project {
	p, _ := ctx.Value(ctxProject).(*storage.Project)
	return p
}

func withProjectRole(ctx context.Context, r *storage.Role) context.Context {
	return context.WithValue(ctx, ctxProjectRole, r)
}

// ProjectRoleFromContext returns the caller's role on the in-scope project,
// or nil if they hold none (global admins may hold none and still pass).
func ProjectRoleFromContext(ctx context.Context) *storage.Role {
	r, _ := ctx.Value(ctxProjectRole).(*storage.Role)
	return r
}

func withAPIToken(ctx context.Context, t *storage.APIToken) context.Context {
	return context.WithValue(ctx, ctxAPIToken, t)
}

// APITokenFromContext returns the API token used to authenticate this
// request, if the request came from a machine client rather than a browser
// session.
func APITokenFromContext(ctx context.Context) *storage.APIToken {
	t, _ := ctx.Value(ctxAPIToken).(*storage.APIToken)
	return t
}

// ActorIsGlobalAdmin reports whether the caller of this request has
// global-admin-equivalent rights: either a session user with
// IsGlobalAdmin, or any API token at all -- every token is global-admin
// equivalent by design (see pkg/auth middleware and specs.md §1).
func ActorIsGlobalAdmin(ctx context.Context) bool {
	if u := UserFromContext(ctx); u != nil {
		return u.IsGlobalAdmin
	}
	return APITokenFromContext(ctx) != nil
}

// ActorLogID returns an identifier for global_logs.user_id representing
// whichever principal (session user or API token) authenticated this
// request, or "" if neither is present (should not happen behind
// requireIdentity/requireGlobalAdmin, but handlers stay defensive).
func ActorLogID(ctx context.Context) string {
	if u := UserFromContext(ctx); u != nil {
		return u.ID
	}
	if t := APITokenFromContext(ctx); t != nil {
		return "token:" + t.ID
	}
	return ""
}
