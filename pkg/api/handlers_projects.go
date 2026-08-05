package api

import (
	"net/http"

	"github.com/google/uuid"

	"tricms/pkg/storage"
)

type projectView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toProjectView(p *storage.Project) projectView {
	return projectView{ID: p.ID, Name: p.Name}
}

// handleListProjects returns every project the caller may access: all of
// them for a global admin, only permissioned ones otherwise (spec §1).
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projects, err := s.System.ListProjectsForUser(r.Context(), user.ID, user.IsGlobalAdmin)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	out := make([]projectView, 0, len(projects))
	for _, p := range projects {
		out = append(out, toProjectView(p))
	}
	writeJSON(w, http.StatusOK, out)
}

type createProjectRequest struct {
	Name string `json:"name"`
}

// handleCreateProject creates a new project: a system.db row plus its
// physical ./data/projects/{id} directory and client.db (spec §4.3).
// Global-admin only.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "field 'name' is required")
		return
	}

	id := "proj_" + uuid.NewString()
	db, err := s.Manager.CreateProjectStorage(id)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	db.Close() // will be lazily reopened & cached on first real use

	proj := &storage.Project{ID: id, Name: req.Name, FolderPath: s.Manager.ProjectDir(id)}
	if err := s.System.CreateProject(r.Context(), proj); err != nil {
		_ = s.Manager.DeleteProjectStorage(id) // roll back the directory we just created
		writeStorageError(w, err)
		return
	}

	user := UserFromContext(r.Context())
	_ = s.System.LogAction(r.Context(), user.ID, proj.ID, "project.create", map[string]string{"name": proj.Name})
	writeJSON(w, http.StatusCreated, toProjectView(proj))
}

type deleteProjectRequest struct {
	ConfirmName string `json:"confirm_name"`
}

// handleDeleteProject irreversibly deletes a project and all of its data.
// Per spec §4.3, this requires the caller to re-type the project's exact
// name as an explicit double-confirmation. Global-admin only.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())

	var req deleteProjectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ConfirmName != project.Name {
		writeError(w, http.StatusBadRequest, "confirm_name must match the exact project name")
		return
	}

	s.forgetProjectDB(project.ID)
	if err := s.Manager.DeleteProjectStorage(project.ID); err != nil {
		writeStorageError(w, err)
		return
	}
	if err := s.System.DeleteProject(r.Context(), project.ID); err != nil {
		writeStorageError(w, err)
		return
	}

	user := UserFromContext(r.Context())
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "project.delete", map[string]string{"name": project.Name})
	w.WriteHeader(http.StatusNoContent)
}
