package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// mountHTMXRoutes wires the server-rendered admin UI (spec §4) on top of the
// same authenticated Server used by the JSON API. It's only mounted when
// s.Templates is non-nil.
func mountHTMXRoutes(r chi.Router, s *Server) {
	if s.StaticFS != nil {
		fileServer := http.FileServer(http.FS(s.StaticFS))
		r.Handle("/static/*", http.StripPrefix("/static/", fileServer))
	}

	r.Get("/login", s.htmxLoginPage)
	r.Post("/login", s.htmxLoginSubmit)
	r.Get("/logout", s.htmxLogout)

	r.Get("/", s.htmxDashboard)
	r.Get("/projects/{projectID}", s.htmxCollections)
	r.Get("/projects/{projectID}/medias", s.htmxMedias)
	r.Get("/projects/{projectID}/users", s.htmxProjectUsers)

	r.Get("/admin", s.htmxAdminRedirect)
	r.Get("/admin/projects", s.htmxAdminProjects)
	r.Get("/admin/users", s.htmxAdminUsers)
	r.Get("/admin/logs", s.htmxAdminLogs)
}

func (s *Server) htmxCurrentUser(r *http.Request) *storage.User {
	return UserFromContext(r.Context())
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- Auth pages ----

func (s *Server) htmxLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.htmxCurrentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	data := &PageData{PageTitle: "Connexion", Content: struct{ Error string }{}}
	s.render(w, "page:login", data)
}

func (s *Server) htmxLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "page:login", &PageData{PageTitle: "Connexion", Content: struct{ Error string }{"Formulaire invalide"}})
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, err := s.System.GetUserByEmail(r.Context(), email)
	if err == nil {
		err = auth.VerifyPassword(user.PasswordHash, password)
	}
	if err != nil {
		s.render(w, "page:login", &PageData{PageTitle: "Connexion", Content: struct{ Error string }{"Identifiants invalides"}})
		return
	}

	token, err := s.Issuer.Issue(user.ID, user.IsGlobalAdmin)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.SessionCookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(24 * time.Hour),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) htmxLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: s.SessionCookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: time.Unix(0, 0), MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- Dashboard ----

func (s *Server) htmxDashboard(w http.ResponseWriter, r *http.Request) {
	user := s.htmxCurrentUser(r)
	if user == nil {
		redirectToLogin(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "Mes projets", nil)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, "page:dashboard", data)
}

// loadProjectForHTMX resolves {projectID}, returning (nil,nil) with the
// response already written if the caller must stop (redirect/403/404).
func (s *Server) loadProjectForHTMX(w http.ResponseWriter, r *http.Request, section auth.Section) (*storage.User, *storage.Project) {
	user := s.htmxCurrentUser(r)
	if user == nil {
		redirectToLogin(w, r)
		return nil, nil
	}
	project, err := s.System.GetProject(r.Context(), chi.URLParam(r, "projectID"))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return nil, nil
	}

	var role *storage.Role
	if !user.IsGlobalAdmin {
		pp, err := s.System.GetProjectPermission(r.Context(), user.ID, project.ID)
		if err == nil {
			role = &pp.Role
		}
	}
	if !auth.CanAccessSection(user.IsGlobalAdmin, role, section) {
		http.Error(w, "403 forbidden", http.StatusForbidden)
		return nil, nil
	}
	return user, project
}

// ---- Project pages ----

func (s *Server) htmxCollections(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
	if user == nil {
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	schemas, err := db.ListSchemas(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data, err := s.buildPageData(r.Context(), user, project, "Collections", struct{ Schemas []*storage.Schema }{schemas})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, "page:collections", data)
}

func (s *Server) htmxMedias(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionMedias)
	if user == nil {
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	medias, err := db.ListMedias(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data, err := s.buildPageData(r.Context(), user, project, "Médias", struct{ Medias []*storage.Media }{medias})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, "page:medias", data)
}

func (s *Server) htmxProjectUsers(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionUsers)
	if user == nil {
		return
	}
	perms, err := s.System.ListProjectPermissions(r.Context(), project.ID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
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
	data, err := s.buildPageData(r.Context(), user, project, "Utilisateurs", struct{ Permissions []projectPermissionView }{views})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, "page:project_users", data)
}

// ---- Administration pages (global admin only) ----

func (s *Server) htmxAdminRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
}

func (s *Server) requireHTMXGlobalAdmin(w http.ResponseWriter, r *http.Request) *storage.User {
	user := s.htmxCurrentUser(r)
	if user == nil {
		redirectToLogin(w, r)
		return nil
	}
	if !user.IsGlobalAdmin {
		http.Error(w, "403 forbidden", http.StatusForbidden)
		return nil
	}
	return user
}

func (s *Server) htmxAdminProjects(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	projects, err := s.System.ListProjects(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "Administration · Projets", struct{ Projects []*storage.Project }{projects})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, "page:admin_projects", data)
}

func (s *Server) htmxAdminUsers(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	users, err := s.System.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "Administration · Comptes", struct{ Users []*storage.User }{users})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, "page:admin_users", data)
}

func (s *Server) htmxAdminLogs(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	logs, err := s.System.ListLogs(r.Context(), 200)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "Administration · Logs", struct{ Logs []*storage.GlobalLog }{logs})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, "page:admin_logs", data)
}
