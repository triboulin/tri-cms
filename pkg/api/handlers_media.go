package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/storage"
)

// handleListMedias lists every media asset of the in-scope project.
// Available to any project role.
func (s *Server) handleListMedias(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	medias, err := db.ListMedias(r.Context())
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, medias)
}

// handleUploadMedia stores an uploaded file under the project's isolated
// media directory (spec §2.2: `_medias.file_path` relative to
// ./data/projects/{project_id}/media/) and records it in client.db.
// REDACTEUR+ only.
func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, s.MaxUploadSize)
	if err := r.ParseMultipartForm(multipartMemoryThreshold); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or malformed upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' form field")
		return
	}
	defer file.Close()

	id := uuid.NewString()
	storedName := id + filepath.Ext(header.Filename)
	destPath := filepath.Join(s.Manager.ProjectMediaDir(project.ID), storedName)

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store file")
		return
	}
	written, err := io.Copy(out, file)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		os.Remove(destPath)
		writeError(w, http.StatusInternalServerError, "could not store file")
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	db, err := s.projectDB(project.ID)
	if err != nil {
		os.Remove(destPath)
		writeStorageError(w, err)
		return
	}
	m := &storage.Media{
		ID:       id,
		Filename: header.Filename,
		MimeType: mimeType,
		Size:     written,
		FilePath: storedName,
	}
	if err := db.CreateMedia(r.Context(), m); err != nil {
		os.Remove(destPath)
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// handleDeleteMedia removes a media asset's DB record and its underlying
// file. REDACTEUR+ only.
func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	mediaID := chi.URLParam(r, "mediaID")

	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	m, err := db.GetMedia(r.Context(), mediaID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	if err := db.DeleteMedia(r.Context(), mediaID); err != nil {
		writeStorageError(w, err)
		return
	}
	_ = os.Remove(filepath.Join(s.Manager.ProjectMediaDir(project.ID), m.FilePath))
	w.WriteHeader(http.StatusNoContent)
}
