package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

func (s *Server) htmxAdminProjects(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	projects, err := s.System.ListProjects(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "admin_projects", "", struct{ Projects []*storage.Project }{projects})
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:admin_projects", data)
}

// htmxAdminDeleteProject deletes a project, requiring the caller to re-type
// its exact name as an explicit double confirmation (spec §4.3).
func (s *Server) htmxAdminDeleteProject(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	back := "/admin/projects"
	projectID := chi.URLParam(r, "projectID")

	project, err := s.System.GetProject(r.Context(), projectID)
	if err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	if r.FormValue("confirm_name") != project.Name {
		redirectWithFlash(w, r, back, "Le nom saisi ne correspond pas au projet « "+project.Name+" ».", "error")
		return
	}

	s.forgetProjectDB(project.ID)
	if err := s.Manager.DeleteProjectStorage(project.ID); err != nil {
		redirectWithFlash(w, r, back, "Suppression impossible : "+err.Error(), "error")
		return
	}
	if err := s.System.DeleteProject(r.Context(), project.ID); err != nil {
		redirectWithFlash(w, r, back, "Suppression impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "project.delete", map[string]string{"name": project.Name})
	redirectWithFlash(w, r, back, "Projet « "+project.Name+" » supprimé.", "success")
}

func (s *Server) htmxAdminUsers(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	users, err := s.System.ListUsers(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "admin_users", "", struct{ Users []*storage.User }{users})
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:admin_users", data)
}

func (s *Server) htmxAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	admin := s.requireHTMXGlobalAdmin(w, r)
	if admin == nil {
		return
	}
	back := "/admin/users"
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")
	if email == "" || password == "" {
		redirectWithFlash(w, r, back, "Email et mot de passe requis.", "error")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		redirectWithFlash(w, r, back, err.Error(), "error")
		return
	}
	isAdmin := r.FormValue("is_global_admin") == "true"
	u := &storage.User{ID: uuid.NewString(), Email: email, PasswordHash: hash, IsGlobalAdmin: isAdmin}
	if err := s.System.CreateUser(r.Context(), u); err != nil {
		redirectWithFlash(w, r, back, "Création impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), admin.ID, "", "user.create", map[string]string{"email": email})
	redirectWithFlash(w, r, back, "Utilisateur « "+email+" » créé.", "success")
}

func (s *Server) htmxAdminToggleUserAdmin(w http.ResponseWriter, r *http.Request) {
	admin := s.requireHTMXGlobalAdmin(w, r)
	if admin == nil {
		return
	}
	back := "/admin/users"
	userID := chi.URLParam(r, "userID")
	target, err := s.System.GetUserByID(r.Context(), userID)
	if err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	if err := s.System.SetGlobalAdmin(r.Context(), userID, !target.IsGlobalAdmin); err != nil {
		redirectWithFlash(w, r, back, "Mise à jour impossible : "+err.Error(), "error")
		return
	}
	redirectWithFlash(w, r, back, "Statut administrateur mis à jour pour "+target.Email+".", "success")
}

func (s *Server) htmxAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	admin := s.requireHTMXGlobalAdmin(w, r)
	if admin == nil {
		return
	}
	back := "/admin/users"
	userID := chi.URLParam(r, "userID")
	if err := s.System.DeleteUser(r.Context(), userID); err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	_ = s.System.LogAction(r.Context(), admin.ID, "", "user.delete", map[string]string{"user_id": userID})
	redirectWithFlash(w, r, back, "Utilisateur supprimé.", "success")
}

// ---- Global rights matrix (spec §4.3) ----

type projectPermissionsBlock struct {
	Project     *storage.Project
	Permissions []projectPermissionView
}

func (s *Server) htmxAdminPermissions(w http.ResponseWriter, r *http.Request) {
	admin := s.requireHTMXGlobalAdmin(w, r)
	if admin == nil {
		return
	}
	projects, err := s.System.ListProjects(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	users, err := s.System.ListUsers(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}

	blocks := make([]projectPermissionsBlock, 0, len(projects))
	for _, p := range projects {
		perms, err := s.System.ListProjectPermissions(r.Context(), p.ID)
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
		blocks = append(blocks, projectPermissionsBlock{Project: p, Permissions: views})
	}

	content := struct {
		Blocks []projectPermissionsBlock
		Users  []*storage.User
		Roles  []storage.Role
	}{blocks, users, []storage.Role{storage.RoleConcepteur, storage.RoleGestionnaire, storage.RoleRedacteur}}

	data, err := s.buildPageData(r.Context(), admin, nil, "admin_permissions", "", content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:admin_permissions", data)
}

func (s *Server) htmxAdminAssignPermission(w http.ResponseWriter, r *http.Request) {
	admin := s.requireHTMXGlobalAdmin(w, r)
	if admin == nil {
		return
	}
	back := "/admin/permissions"
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	projectID := r.FormValue("project_id")
	email := r.FormValue("email")
	role := storage.Role(r.FormValue("role"))
	if projectID == "" || email == "" || !role.Valid() {
		redirectWithFlash(w, r, back, "Projet, email et rôle sont requis.", "error")
		return
	}
	target, err := s.System.GetUserByEmail(r.Context(), email)
	if err != nil {
		redirectWithFlash(w, r, back, "Aucun utilisateur avec cet email.", "error")
		return
	}
	if err := s.System.SetProjectPermission(r.Context(), &storage.ProjectPermission{UserID: target.ID, ProjectID: projectID, Role: role}); err != nil {
		redirectWithFlash(w, r, back, "Attribution impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), admin.ID, projectID, "project_permission.set", map[string]string{"target_user": target.ID, "role": string(role)})
	redirectWithFlash(w, r, back, "Rôle "+string(role)+" attribué à "+email+".", "success")
}

func (s *Server) htmxAdminRemovePermission(w http.ResponseWriter, r *http.Request) {
	admin := s.requireHTMXGlobalAdmin(w, r)
	if admin == nil {
		return
	}
	back := "/admin/permissions"
	projectID := chi.URLParam(r, "projectID")
	userID := chi.URLParam(r, "userID")
	if err := s.System.DeleteProjectPermission(r.Context(), userID, projectID); err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	_ = s.System.LogAction(r.Context(), admin.ID, projectID, "project_permission.delete", map[string]string{"target_user": userID})
	redirectWithFlash(w, r, back, "Accès retiré.", "success")
}
