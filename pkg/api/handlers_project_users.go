package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

type projectPermissionView struct {
	UserID string       `json:"user_id"`
	Email  string       `json:"email"`
	Role   storage.Role `json:"role"`
}

// handleListProjectUsers lists everyone with a role on the in-scope
// project. Requires GESTIONNAIRE+ (or global admin), spec §4.4.
func (s *Server) handleListProjectUsers(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	perms, err := s.System.ListProjectPermissions(r.Context(), project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	out := make([]projectPermissionView, 0, len(perms))
	for _, pp := range perms {
		email := ""
		if u, err := s.System.GetUserByID(r.Context(), pp.UserID); err == nil {
			email = u.Email
		}
		out = append(out, projectPermissionView{UserID: pp.UserID, Email: email, Role: pp.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

type assignProjectUserRequest struct {
	Email string       `json:"email"`
	Role  storage.Role `json:"role"`
}

// handleAssignProjectUser grants (or changes) a project role for a user
// identified by email. Per spec §4.4, a GESTIONNAIRE (or a CONCEPTEUR
// using the same view) may only grant GESTIONNAIRE or REDACTEUR -- never
// CONCEPTEUR, which is reserved for ADMIN via the global rights matrix.
func (s *Server) handleAssignProjectUser(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	actorRole := ProjectRoleFromContext(r.Context())

	var req assignProjectUserRequest
	if err := decodeJSON(r, &req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "fields 'email' and 'role' are required")
		return
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}
	if !auth.CanAssignRole(ActorIsGlobalAdmin(r.Context()), actorRole, req.Role) {
		writeError(w, http.StatusForbidden, "forbidden: cannot assign this role")
		return
	}

	target, err := s.System.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	if err := s.System.SetProjectPermission(r.Context(), &storage.ProjectPermission{
		UserID:    target.ID,
		ProjectID: project.ID,
		Role:      req.Role,
	}); err != nil {
		writeStorageError(w, err)
		return
	}

	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), project.ID, "project_permission.set", map[string]string{
		"target_user": target.ID, "role": string(req.Role),
	})
	writeJSON(w, http.StatusOK, projectPermissionView{UserID: target.ID, Email: target.Email, Role: req.Role})
}

// handleRemoveProjectUser revokes a user's role on the in-scope project.
// The actor must be authorized to manage the role tier being removed
// (same rule as assignment: GESTIONNAIRE-tier actors cannot touch a
// CONCEPTEUR's permission row).
func (s *Server) handleRemoveProjectUser(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	actorRole := ProjectRoleFromContext(r.Context())
	targetUserID := chi.URLParam(r, "userID")

	existing, err := s.System.GetProjectPermission(r.Context(), targetUserID, project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	if !auth.CanAssignRole(ActorIsGlobalAdmin(r.Context()), actorRole, existing.Role) {
		writeError(w, http.StatusForbidden, "forbidden: cannot manage this user's role")
		return
	}

	if err := s.System.DeleteProjectPermission(r.Context(), targetUserID, project.ID); err != nil {
		writeStorageError(w, err)
		return
	}
	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), project.ID, "project_permission.delete", map[string]string{"target_user": targetUserID})
	w.WriteHeader(http.StatusNoContent)
}
