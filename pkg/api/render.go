package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// SidebarVisibility mirrors spec §4.2's per-section role gating, precomputed
// once per request so templates stay dumb (`{{if .Sidebar.Conception}}`).
// It's still used to decide which breadcrumb-level-2 sections are offered.
type SidebarVisibility struct {
	Conception  bool
	Collections bool
	Medias      bool
	Users       bool
	API         bool
	Webhooks    bool
	Logs        bool
}

// NavItem is one entry in a breadcrumb hover-dropdown: a sibling destination
// at the same nesting level as the crumb being hovered.
type NavItem struct {
	Key    string
	Label  string
	Icon   string
	URL    string
	Active bool
}

// projectSectionCatalog lists the level-2 breadcrumb siblings for a project
// (Conception / Collections / Médias / Utilisateurs / API / Webhooks),
// filtered by what the current user/role may access.
func projectSectionCatalog(project *storage.Project, sidebar SidebarVisibility, activeKey string) []NavItem {
	if project == nil {
		return nil
	}
	base := "/projects/" + project.ID
	var items []NavItem
	add := func(key, label, icon, suffix string, visible bool) {
		if !visible {
			return
		}
		items = append(items, NavItem{Key: key, Label: label, Icon: icon, URL: base + suffix, Active: key == activeKey})
	}
	add("conception", "Conception", "design_services", "/conception", sidebar.Conception)
	add("collections", "Collections", "table_view", "", sidebar.Collections)
	add("medias", "Médias", "perm_media", "/medias", sidebar.Medias)
	add("users", "Utilisateurs", "group", "/users", sidebar.Users)
	add("api", "API", "vpn_key", "/tokens", sidebar.API)
	add("webhooks", "Webhooks", "webhook", "/webhooks", sidebar.Webhooks)
	add("logs", "Logs", "history", "/logs", sidebar.Logs)
	return items
}

// adminSectionCatalog lists the level-2 breadcrumb siblings inside the
// global Administration area (Projets / Comptes / Matrice des droits).
// Logs used to live here too, but the audit trail is project-scoped now
// (still admin-only) -- see projectSectionCatalog / SectionLogs instead.
func adminSectionCatalog(activeKey string) []NavItem {
	return []NavItem{
		{Key: "admin_projects", Label: "Projets", Icon: "folder", URL: "/admin/projects", Active: activeKey == "admin_projects"},
		{Key: "admin_users", Label: "Comptes", Icon: "group", URL: "/admin/users", Active: activeKey == "admin_users"},
		{Key: "admin_permissions", Label: "Matrice des droits", Icon: "rule", URL: "/admin/permissions", Active: activeKey == "admin_permissions"},
	}
}

// PageData is the common view-model passed to every HTMX page template.
type PageData struct {
	User           *storage.User
	CurrentProject *storage.Project
	CurrentRole    *storage.Role // nil for global admins or users with no project role
	Projects       []*storage.Project
	IsAdmin        bool // true when rendering a page under /admin/*
	SectionKey     string
	SectionLabel   string // level-2 breadcrumb label (e.g. "Conception", "Projets")
	SectionIcon    string
	SectionURL     string
	Sections       []NavItem // level-2 breadcrumb dropdown siblings
	Leaf           string    // optional level-3 breadcrumb detail (e.g. a schema name)
	PageTitle      string
	Sidebar        SidebarVisibility
	Content        any
	Flash          string
	FlashKind      string // "success" or "error"
}

// buildPageData assembles the common view-model for every HTMX page.
// sectionKey identifies the current level-2 breadcrumb entry ("conception",
// "collections", "medias", "users", "api", "webhooks", "logs", or
// "admin_projects", "admin_users", "admin_permissions"; empty for the
// dashboard).
// leaf is an optional level-3 breadcrumb detail (e.g. a schema or content
// name) shown after the section; when empty the section itself is the final
// (bold) crumb. On the dashboard, pass leaf as the page title directly.
func (s *Server) buildPageData(ctx context.Context, user *storage.User, project *storage.Project, sectionKey, leaf string, content any) (*PageData, error) {
	projects, err := s.System.ListProjectsForUser(ctx, user.ID, user.IsGlobalAdmin)
	if err != nil {
		return nil, err
	}

	var role *storage.Role
	if project != nil && !user.IsGlobalAdmin {
		pp, err := s.System.GetProjectPermission(ctx, user.ID, project.ID)
		switch {
		case err == nil:
			role = &pp.Role
		case errors.Is(err, storage.ErrNotFound):
			// no role: sections will simply stay hidden.
		default:
			return nil, err
		}
	}

	sidebar := SidebarVisibility{
		Conception:  project != nil && auth.CanAccessSection(user.IsGlobalAdmin, role, auth.SectionConception),
		Collections: project != nil && auth.CanAccessSection(user.IsGlobalAdmin, role, auth.SectionCollections),
		Medias:      project != nil && auth.CanAccessSection(user.IsGlobalAdmin, role, auth.SectionMedias),
		Users:       project != nil && auth.CanAccessSection(user.IsGlobalAdmin, role, auth.SectionUsers),
		API:         project != nil && auth.CanAccessSection(user.IsGlobalAdmin, role, auth.SectionAPI),
		Webhooks:    project != nil && auth.CanAccessSection(user.IsGlobalAdmin, role, auth.SectionWebhooks),
		Logs:        project != nil && auth.CanAccessSection(user.IsGlobalAdmin, role, auth.SectionLogs),
	}

	isAdmin := strings.HasPrefix(sectionKey, "admin_")
	var sections []NavItem
	if isAdmin {
		sections = adminSectionCatalog(sectionKey)
	} else if project != nil {
		sections = projectSectionCatalog(project, sidebar, sectionKey)
	}

	var sectionLabel, sectionIcon, sectionURL string
	for _, it := range sections {
		if it.Active {
			sectionLabel, sectionIcon, sectionURL = it.Label, it.Icon, it.URL
		}
	}

	pageTitle := sectionLabel
	if leaf != "" {
		pageTitle = leaf
	}

	return &PageData{
		User:           user,
		CurrentProject: project,
		CurrentRole:    role,
		Projects:       projects,
		IsAdmin:        isAdmin,
		SectionKey:     sectionKey,
		SectionLabel:   sectionLabel,
		SectionIcon:    sectionIcon,
		SectionURL:     sectionURL,
		Sections:       sections,
		Leaf:           leaf,
		PageTitle:      pageTitle,
		Sidebar:        sidebar,
		Content:        content,
	}, nil
}

func (s *Server) render(w http.ResponseWriter, name string, data *PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.Templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template render error (%s): %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// renderError renders a styled standalone error page (500/404) in the
// app's own visual identity, instead of the bare text http.Error produces --
// which, for a non-technical user, looks like the whole application broke
// rather than "something went wrong, here's what to do".
func (s *Server) renderError(w http.ResponseWriter, status int, title, message string) {
	if s.Templates == nil {
		http.Error(w, title, status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.Templates.ExecuteTemplate(w, "page:error", struct{ Title, Message string }{title, message}); err != nil {
		log.Printf("template render error (page:error): %v", err)
	}
}

// htmxServerError is the HTMX-UI equivalent of writeStorageError's 500 path:
// every htmx_*.go handler that used to call
// http.Error(w, "internal server error", http.StatusInternalServerError)
// now calls this instead.
func (s *Server) htmxServerError(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, http.StatusInternalServerError, "Erreur serveur", "Une erreur inattendue s'est produite. Réessayez dans un instant ; si le problème persiste, contactez un administrateur.")
}

// htmxNotFoundPage renders a styled 404 for the HTMX UI (as opposed to the
// JSON API's plain 404, handled separately).
func (s *Server) htmxNotFoundPage(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, http.StatusNotFound, "Page introuvable", "Cette page n'existe pas ou plus.")
}

// applyFlash copies ?flash=...&flash_kind=... query params (set by
// redirectWithFlash) onto data, so the post/redirect/get pattern used by
// every HTMX form can surface a success/error message after the redirect.
func applyFlash(r *http.Request, data *PageData) {
	data.Flash = r.URL.Query().Get("flash")
	data.FlashKind = r.URL.Query().Get("flash_kind")
	if data.FlashKind == "" {
		data.FlashKind = "success"
	}
}

// redirectWithFlash redirects to path (typically the referring list page)
// carrying a short human-readable status message, per the standard
// Post/Redirect/Get pattern used by every mutating HTMX form handler.
func redirectWithFlash(w http.ResponseWriter, r *http.Request, path, message, kind string) {
	u := path
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	if message != "" {
		u += sep + "flash=" + url.QueryEscape(message) + "&flash_kind=" + url.QueryEscape(kind)
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}
