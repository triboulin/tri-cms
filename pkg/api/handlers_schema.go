package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

// ---- Folders ----

type folderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id,omitempty"`
}

// handleListFolders lists the folder tree (flat) of the in-scope project.
// Available to any project role (read-only), spec §4.2 "Collections".
func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	folders, err := db.ListFolders(r.Context())
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

// handleCreateFolder creates a folder. CONCEPTEUR+ only (spec §4.2
// "Conception").
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	var req folderRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "field 'name' is required")
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	f := &storage.Folder{ID: uuid.NewString(), Name: req.Name, ParentID: req.ParentID}
	if err := db.CreateFolder(r.Context(), f); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

// handleDeleteFolder deletes a folder (and, via ON DELETE CASCADE,
// everything nested under it -- spec §2.2 warns this must be an explicit,
// confirmed action at the UI layer). CONCEPTEUR+ only.
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	folderID := chi.URLParam(r, "folderID")
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	if err := db.DeleteFolder(r.Context(), folderID); err != nil {
		writeStorageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Schemas ----

type schemaRequest struct {
	Slug       string               `json:"slug"`
	Name       string               `json:"name"`
	FolderID   *string              `json:"folder_id,omitempty"`
	Definition trischema.Definition `json:"definition"`
}

// handleListSchemas lists every content schema of the in-scope project.
// Available to any project role.
func (s *Server) handleListSchemas(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	schemas, err := db.ListSchemas(r.Context())
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemas)
}

// handleGetSchema fetches one schema by slug.
func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	slug := chi.URLParam(r, "schemaSlug")
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	sc, err := db.GetSchema(r.Context(), slug)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

// handleCreateSchema creates a new content schema/collection. Its `slug`
// becomes immutable (spec §2.2) and is used directly in API URLs.
// CONCEPTEUR+ only.
func (s *Server) handleCreateSchema(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	var req schemaRequest
	if err := decodeJSON(r, &req); err != nil || req.Slug == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "fields 'slug' and 'name' are required")
		return
	}
	if err := req.Definition.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defJSON, err := marshalDefinition(req.Definition)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode definition")
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	sc := &storage.Schema{Slug: req.Slug, Name: req.Name, FolderID: req.FolderID, Definition: defJSON}
	if err := db.CreateSchema(r.Context(), sc); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sc)
}

// handleUpdateSchema updates a schema's name/folder/definition. The slug
// path parameter is authoritative and can never be changed here (spec §2.2).
// CONCEPTEUR+ only.
func (s *Server) handleUpdateSchema(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	slug := chi.URLParam(r, "schemaSlug")

	var req schemaRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "field 'name' is required")
		return
	}
	if err := req.Definition.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defJSON, err := marshalDefinition(req.Definition)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode definition")
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	sc := &storage.Schema{Slug: slug, Name: req.Name, FolderID: req.FolderID, Definition: defJSON}
	if err := db.UpdateSchema(r.Context(), sc); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

// handleDeleteSchema deletes a schema and, via ON DELETE CASCADE, every
// content instance stored under it. CONCEPTEUR+ only.
func (s *Server) handleDeleteSchema(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	slug := chi.URLParam(r, "schemaSlug")
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	if err := db.DeleteSchema(r.Context(), slug); err != nil {
		writeStorageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func marshalDefinition(def trischema.Definition) (string, error) {
	b, err := json.Marshal(def)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
