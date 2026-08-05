package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

func (s *Server) htmxProjectUsers(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionUsers)
	if user == nil {
		return
	}
	perms, err := s.System.ListProjectPermissions(r.Context(), project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	views := make([]projectPermissionView, 0, len(perms))
	for _, pp := range perms {
		email := ""
		if u, err := s.System.GetUserByID(r.Context(), pp.UserID); err == nil {
			email = u.Email
		}
		views = append(views, projectPermissionView{UserID: pp.UserID, Email: email, Role: pp.Role})
	}
	// The project "Utilisateurs" view can only ever grant GESTIONNAIRE or
	// REDACTEUR (spec §4.4) -- CONCEPTEUR is admin-only via /admin/permissions.
	// IsAdmin drives the visibility of the explanatory sentence pointing at
	// that global matrix: it's meaningless (and previously confusing) noise
	// for the non-admin GESTIONNAIRE who actually uses this page day to day.
	content := struct {
		Permissions     []projectPermissionView
		AssignableRoles []storage.Role
		IsAdmin         bool
	}{views, []storage.Role{storage.RoleGestionnaire, storage.RoleRedacteur}, user.IsGlobalAdmin}

	data, err := s.buildPageData(r.Context(), user, project, "users", "", content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:project_users", data)
}

// htmxAssignProjectUser grants a project role to a user identified by email.
// If no account with that email exists yet and a password was supplied, it
// creates the account first (as a plain, non-global-admin user) instead of
// forcing the caller to go create it separately via /admin/users/create --
// which a GESTIONNAIRE can't even reach.
func (s *Server) htmxAssignProjectUser(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionUsers)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/users"
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")
	role := storage.Role(r.FormValue("role"))
	if email == "" || !role.Valid() {
		redirectWithFlash(w, r, back, "Email et rôle valides requis.", "error")
		return
	}

	var actorRole *storage.Role
	if !user.IsGlobalAdmin {
		if pp, err := s.System.GetProjectPermission(r.Context(), user.ID, project.ID); err == nil {
			actorRole = &pp.Role
		}
	}
	if !auth.CanAssignRole(user.IsGlobalAdmin, actorRole, role) {
		redirectWithFlash(w, r, back, "Vous ne pouvez pas attribuer ce rôle depuis cette vue.", "error")
		return
	}

	target, err := s.System.GetUserByEmail(r.Context(), email)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			s.htmxServerError(w, r)
			return
		}
		if password == "" {
			redirectWithFlash(w, r, back, "Aucun compte avec cet email. Renseignez un mot de passe pour en créer un.", "error")
			return
		}
		hash, hashErr := auth.HashPassword(password)
		if hashErr != nil {
			redirectWithFlash(w, r, back, hashErr.Error(), "error")
			return
		}
		target = &storage.User{ID: uuid.NewString(), Email: email, PasswordHash: hash}
		if err := s.System.CreateUser(r.Context(), target); err != nil {
			redirectWithFlash(w, r, back, "Création du compte impossible : "+err.Error(), "error")
			return
		}
		_ = s.System.LogAction(r.Context(), user.ID, project.ID, "user.create", map[string]string{"email": email})
	}
	if err := s.System.SetProjectPermission(r.Context(), &storage.ProjectPermission{UserID: target.ID, ProjectID: project.ID, Role: role}); err != nil {
		redirectWithFlash(w, r, back, "Attribution impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "project_permission.set", map[string]string{"target_user": target.ID, "role": string(role)})
	redirectWithFlash(w, r, back, "Rôle "+string(role)+" attribué à "+email+".", "success")
}

// htmxUpdateProjectUserRole changes an existing member's project role
// in-place (instead of forcing a remove-then-reassign round trip). The
// caller must be allowed to assign *both* the member's current role and the
// requested one, so a GESTIONNAIRE can't use this to touch a CONCEPTEUR
// (which stays admin-only via /admin/permissions).
func (s *Server) htmxUpdateProjectUserRole(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionUsers)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/users"
	targetUserID := chi.URLParam(r, "userID")
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	newRole := storage.Role(r.FormValue("role"))
	if !newRole.Valid() {
		redirectWithFlash(w, r, back, "Rôle invalide.", "error")
		return
	}
	existing, err := s.System.GetProjectPermission(r.Context(), targetUserID, project.ID)
	if err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	var actorRole *storage.Role
	if !user.IsGlobalAdmin {
		if pp, err := s.System.GetProjectPermission(r.Context(), user.ID, project.ID); err == nil {
			actorRole = &pp.Role
		}
	}
	if !auth.CanAssignRole(user.IsGlobalAdmin, actorRole, existing.Role) || !auth.CanAssignRole(user.IsGlobalAdmin, actorRole, newRole) {
		redirectWithFlash(w, r, back, "Vous ne pouvez pas modifier ce rôle depuis cette vue.", "error")
		return
	}
	if err := s.System.SetProjectPermission(r.Context(), &storage.ProjectPermission{UserID: targetUserID, ProjectID: project.ID, Role: newRole}); err != nil {
		redirectWithFlash(w, r, back, "Mise à jour impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "project_permission.set", map[string]string{"target_user": targetUserID, "role": string(newRole)})
	redirectWithFlash(w, r, back, "Rôle mis à jour.", "success")
}

func (s *Server) htmxRemoveProjectUser(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionUsers)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/users"
	targetUserID := chi.URLParam(r, "userID")

	existing, err := s.System.GetProjectPermission(r.Context(), targetUserID, project.ID)
	if err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	var actorRole *storage.Role
	if !user.IsGlobalAdmin {
		if pp, err := s.System.GetProjectPermission(r.Context(), user.ID, project.ID); err == nil {
			actorRole = &pp.Role
		}
	}
	if !auth.CanAssignRole(user.IsGlobalAdmin, actorRole, existing.Role) {
		redirectWithFlash(w, r, back, "Vous ne pouvez pas retirer ce rôle depuis cette vue.", "error")
		return
	}
	if err := s.System.DeleteProjectPermission(r.Context(), targetUserID, project.ID); err != nil {
		redirectWithFlash(w, r, back, "Suppression impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "project_permission.delete", map[string]string{"target_user": targetUserID})
	redirectWithFlash(w, r, back, "Accès retiré.", "success")
}
