package api

import (
	"context"
	"errors"
	"log"
	"net/http"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// SidebarVisibility mirrors spec §4.2's per-section role gating, precomputed
// once per request so templates stay dumb (`{{if .Sidebar.Conception}}`).
type SidebarVisibility struct {
	Conception  bool
	Collections bool
	Medias      bool
	Users       bool
	API         bool
	Webhooks    bool
	Logs        bool
}

// PageData is the common view-model passed to every HTMX page template.
type PageData struct {
	User           *storage.User
	CurrentProject *storage.Project
	Projects       []*storage.Project
	PageTitle      string
	Sidebar        SidebarVisibility
	Content        any
}

func (s *Server) buildPageData(ctx context.Context, user *storage.User, project *storage.Project, pageTitle string, content any) (*PageData, error) {
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
			// no role: sidebar sections will simply stay hidden.
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
		Logs:        user.IsGlobalAdmin,
	}

	return &PageData{
		User:           user,
		CurrentProject: project,
		Projects:       projects,
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
