package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// mountHTMXRoutes wires the server-rendered admin UI (spec §4) on top of the
// same authenticated Server used by the JSON API. It's only mounted when
// s.Templates is non-nil. Every mutating route follows the Post/Redirect/Get
// pattern: a POST handler performs the change, then redirects back to the
// relevant list page with a flash message (see render.go/redirectWithFlash),
// so CRUD actions are reachable as plain forms/links, no JS required.
func mountHTMXRoutes(r chi.Router, s *Server) {
	if s.StaticFS != nil {
		fileServer := http.FileServer(http.FS(s.StaticFS))
		r.Handle("/static/*", http.StripPrefix("/static/", fileServer))
	}

	r.Get("/login", s.htmxLoginPage)
	r.Post("/login", s.htmxLoginSubmit)
	r.Get("/logout", s.htmxLogout)

	r.Get("/", s.htmxDashboard)
	r.Post("/projects/create", s.htmxCreateProject)

	r.Get("/projects/{projectID}", s.htmxCollections)

	r.Get("/projects/{projectID}/conception", s.htmxConception)
	r.Post("/projects/{projectID}/schemas/create", s.htmxCreateSchema)
	r.Get("/projects/{projectID}/schemas/{schemaSlug}/edit", s.htmxEditSchemaPage)
	r.Post("/projects/{projectID}/schemas/{schemaSlug}/update", s.htmxUpdateSchema)
	r.Post("/projects/{projectID}/schemas/{schemaSlug}/delete", s.htmxDeleteSchema)

	r.Get("/projects/{projectID}/schemas/{schemaSlug}/contents", s.htmxContentList)
	r.Get("/projects/{projectID}/schemas/{schemaSlug}/contents/new", s.htmxContentForm)
	r.Post("/projects/{projectID}/schemas/{schemaSlug}/contents/create", s.htmxCreateContent)
	r.Get("/projects/{projectID}/schemas/{schemaSlug}/contents/{contentID}/edit", s.htmxContentForm)
	r.Post("/projects/{projectID}/schemas/{schemaSlug}/contents/{contentID}/update", s.htmxUpdateContent)
	r.Post("/projects/{projectID}/schemas/{schemaSlug}/contents/{contentID}/delete", s.htmxDeleteContent)
	r.Post("/projects/{projectID}/schemas/{schemaSlug}/contents/{contentID}/toggle-status", s.htmxToggleContentStatus)

	r.Get("/projects/{projectID}/medias", s.htmxMedias)
	r.Post("/projects/{projectID}/medias/upload", s.htmxUploadMedia)
	r.Get("/projects/{projectID}/medias/{mediaID}/file", s.htmxMediaFile)
	r.Post("/projects/{projectID}/medias/{mediaID}/delete", s.htmxDeleteMedia)

	r.Get("/projects/{projectID}/users", s.htmxProjectUsers)
	r.Post("/projects/{projectID}/users/assign", s.htmxAssignProjectUser)
	r.Post("/projects/{projectID}/users/{userID}/role", s.htmxUpdateProjectUserRole)
	r.Post("/projects/{projectID}/users/{userID}/remove", s.htmxRemoveProjectUser)

	r.Get("/projects/{projectID}/webhooks", s.htmxWebhooks)
	r.Post("/projects/{projectID}/webhooks/create", s.htmxCreateWebhook)
	r.Post("/projects/{projectID}/webhooks/{webhookID}/update", s.htmxUpdateWebhook)
	r.Post("/projects/{projectID}/webhooks/{webhookID}/delete", s.htmxDeleteWebhook)

	r.Get("/projects/{projectID}/logs", s.htmxProjectLogs)

	r.Get("/admin", s.htmxAdminRedirect)
	r.Get("/admin/projects", s.htmxAdminProjects)
	r.Post("/admin/projects/{projectID}/delete", s.htmxAdminDeleteProject)
	r.Get("/admin/tokens", s.htmxAdminTokens)
	r.Post("/admin/tokens/create", s.htmxAdminCreateToken)
	r.Post("/admin/tokens/{tokenID}/delete", s.htmxAdminDeleteToken)
	r.Get("/admin/users", s.htmxAdminUsers)
	r.Post("/admin/users/create", s.htmxAdminCreateUser)
	r.Post("/admin/users/{userID}/toggle-admin", s.htmxAdminToggleUserAdmin)
	r.Post("/admin/users/{userID}/delete", s.htmxAdminDeleteUser)
	r.Get("/admin/permissions", s.htmxAdminPermissions)
	r.Post("/admin/permissions/assign", s.htmxAdminAssignPermission)
	r.Post("/admin/permissions/{projectID}/{userID}/remove", s.htmxAdminRemovePermission)
}

func (s *Server) htmxCurrentUser(r *http.Request) *storage.User {
	return UserFromContext(r.Context())
}

// redirectToLogin sends an unauthenticated visitor to the login page with a
// neutral explanatory notice, instead of silently landing them there with no
// context (which, for someone whose session just expired mid-use, reads as
// an unexplained bug rather than a normal "please sign in again").
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login?notice=1", http.StatusSeeOther)
}

// ---- Auth pages ----

// loginPageContent is the Content shape for page:login, kept as a single
// named type (rather than ad hoc anonymous structs per call site) so every
// render exposes the same fields to the template regardless of which code
// path produced it.
type loginPageContent struct {
	Error, Notice                     string
	BootstrapEmail, BootstrapPassword string
}

func (s *Server) htmxLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.htmxCurrentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	notice := ""
	if r.URL.Query().Get("notice") == "1" {
		notice = "Veuillez vous connecter pour continuer."
	}
	content := loginPageContent{Notice: notice}
	if s.BootstrapFlagPath != "" {
		if flag, ok := readBootstrapFlag(s.BootstrapFlagPath); ok {
			content.BootstrapEmail = flag.Email
			content.BootstrapPassword = flag.Password
		}
	}
	data := &PageData{PageTitle: "Connexion", Content: content}
	s.render(w, "page:login", data)
}

func (s *Server) htmxLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "page:login", &PageData{PageTitle: "Connexion", Content: loginPageContent{Error: "Formulaire invalide"}})
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, err := s.System.GetUserByEmail(r.Context(), email)
	if err == nil {
		err = auth.VerifyPassword(user.PasswordHash, password)
	}
	if err != nil {
		s.render(w, "page:login", &PageData{PageTitle: "Connexion", Content: loginPageContent{Error: "Identifiants invalides"}})
		return
	}

	token, err := s.Issuer.Issue(user.ID, user.IsGlobalAdmin)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	s.clearBootstrapFlag()
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
	data, err := s.buildPageData(r.Context(), user, nil, "", "Mes projets", nil)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:dashboard", data)
}

// htmxCreateProject creates a project from the dashboard's "Nouveau projet"
// form. Global-admin only -- mirrors handleCreateProject (pkg/api/handlers_projects.go)
// but as a plain form submission with a redirect back to the dashboard.
func (s *Server) htmxCreateProject(w http.ResponseWriter, r *http.Request) {
	user := s.htmxCurrentUser(r)
	if user == nil {
		redirectToLogin(w, r)
		return
	}
	if !user.IsGlobalAdmin {
		http.Error(w, "403 forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, "/", "Formulaire invalide.", "error")
		return
	}
	name := r.FormValue("name")
	if name == "" {
		redirectWithFlash(w, r, "/", "Le nom du projet est requis.", "error")
		return
	}

	id := "proj_" + uuid.NewString()
	db, err := s.Manager.CreateProjectStorage(id)
	if err != nil {
		redirectWithFlash(w, r, "/", "Impossible de créer le projet : "+err.Error(), "error")
		return
	}
	db.Close()

	proj := &storage.Project{ID: id, Name: name, FolderPath: s.Manager.ProjectDir(id)}
	if err := s.System.CreateProject(r.Context(), proj); err != nil {
		_ = s.Manager.DeleteProjectStorage(id)
		redirectWithFlash(w, r, "/", "Impossible de créer le projet : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, proj.ID, "project.create", map[string]string{"name": proj.Name})

	redirectWithFlash(w, r, "/projects/"+proj.ID, "Projet « "+name+" » créé.", "success")
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
			s.htmxServerError(w, r)
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

// ---- Project home (Collections index) ----

func (s *Server) htmxCollections(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
	if user == nil {
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	schemas, err := db.ListSchemas(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, project, "collections", "", struct{ Schemas []*storage.Schema }{schemas})
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:collections", data)
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

func (s *Server) htmxAdminRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
}
