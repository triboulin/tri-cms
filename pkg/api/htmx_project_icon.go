package api

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"tricms/pkg/auth"
)

// maxProjectIconSize caps the icon upload well below the general
// MaxUploadSize: this is a small breadcrumb/dashboard-card glyph, not
// project media, so there's no reason to allow a multi-megabyte file here.
const maxProjectIconSize = 2 << 20 // 2 MiB

// htmxProjectIcon serves a project's custom icon (breadcrumb/dashboard
// card). Gated by loadProjectForHTMXAny -- any project member may see it,
// not just CONCEPTEUR+ (matching where it's actually displayed: the
// breadcrumb on every page).
func (s *Server) htmxProjectIcon(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMXAny(w, r)
	if user == nil {
		return
	}
	if project.IconPath == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.Manager.ProjectDir(project.ID), project.IconPath)
	contentType := mime.TypeByExtension(filepath.Ext(project.IconPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

// htmxUploadProjectIcon replaces the project's icon. Gated by
// SectionConception (project structure/identity, same authority level as
// creating schemas), unlike viewing it above.
func (s *Server) htmxUploadProjectIcon(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionConception)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/conception"

	r.Body = http.MaxBytesReader(w, r.Body, maxProjectIconSize)
	if err := r.ParseMultipartForm(multipartMemoryThreshold); err != nil {
		redirectWithFlash(w, r, back, "Fichier trop volumineux (2 Mo max) ou envoi invalide.", "error")
		return
	}
	file, header, err := r.FormFile("icon")
	if err != nil {
		redirectWithFlash(w, r, back, "Sélectionnez une image à téléverser.", "error")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExt := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".svg": true, ".webp": true, ".gif": true}
	if !allowedExt[ext] {
		redirectWithFlash(w, r, back, "Formats acceptés : PNG, JPG, SVG, WebP, GIF.", "error")
		return
	}

	storedName := "icon_" + uuid.NewString() + ext
	destPath := filepath.Join(s.Manager.ProjectDir(project.ID), storedName)
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		redirectWithFlash(w, r, back, "Impossible d'enregistrer le fichier.", "error")
		return
	}
	_, err = out.ReadFrom(file)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		os.Remove(destPath)
		redirectWithFlash(w, r, back, "Impossible d'enregistrer le fichier.", "error")
		return
	}

	previous := project.IconPath
	if err := s.System.SetProjectIcon(r.Context(), project.ID, storedName); err != nil {
		os.Remove(destPath)
		redirectWithFlash(w, r, back, "Impossible d'enregistrer l'icône : "+err.Error(), "error")
		return
	}
	if previous != "" {
		_ = os.Remove(filepath.Join(s.Manager.ProjectDir(project.ID), previous))
	}
	redirectWithFlash(w, r, back, "Icône du projet mise à jour.", "success")
}

// htmxDeleteProjectIcon reverts a project to the default triCMS logo.
func (s *Server) htmxDeleteProjectIcon(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionConception)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/conception"
	if project.IconPath == "" {
		redirectWithFlash(w, r, back, "Ce projet n'a pas d'icône personnalisée.", "error")
		return
	}
	previous := project.IconPath
	if err := s.System.SetProjectIcon(r.Context(), project.ID, ""); err != nil {
		redirectWithFlash(w, r, back, "Erreur serveur.", "error")
		return
	}
	_ = os.Remove(filepath.Join(s.Manager.ProjectDir(project.ID), previous))
	redirectWithFlash(w, r, back, "Icône du projet supprimée.", "success")
}
