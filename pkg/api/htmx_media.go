package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// mediaRowVM decorates a stored media with view-only fields: a human-sized
// byte count, whether it can be shown as an <img> preview, a fallback
// Material icon for non-image types, and the URL serving its raw bytes.
type mediaRowVM struct {
	*storage.Media
	SizeHuman  string
	IsImage    bool
	IsVideo    bool
	Icon       string
	PreviewURL string
}

// humanizeBytes formats a byte count the way a person reads it (o/Ko/Mo/Go),
// rounded to one decimal past the first unit.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d o", n)
	}
	div, exp := int64(unit), 0
	for n2 := n / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	units := []string{"Ko", "Mo", "Go", "To"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}

// mediaIcon picks a Material Symbols icon representing a mime type family,
// for media that can't be shown as an image thumbnail.
func mediaIcon(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "video/"):
		return "videocam"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audiotrack"
	case mimeType == "application/pdf":
		return "picture_as_pdf"
	case strings.HasPrefix(mimeType, "text/"):
		return "description"
	case strings.Contains(mimeType, "zip") || strings.Contains(mimeType, "compressed"):
		return "folder_zip"
	default:
		return "insert_drive_file"
	}
}

func (s *Server) buildMediaRowVMs(project *storage.Project, medias []*storage.Media) []mediaRowVM {
	rows := make([]mediaRowVM, 0, len(medias))
	for _, m := range medias {
		isImage := strings.HasPrefix(m.MimeType, "image/")
		isVideo := strings.HasPrefix(m.MimeType, "video/")
		rows = append(rows, mediaRowVM{
			Media:      m,
			SizeHuman:  humanizeBytes(m.Size),
			IsImage:    isImage,
			IsVideo:    isVideo,
			Icon:       mediaIcon(m.MimeType),
			PreviewURL: "/projects/" + project.ID + "/medias/" + m.ID + "/file",
		})
	}
	return rows
}

func (s *Server) htmxMedias(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionMedias)
	if user == nil {
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	medias, err := db.ListMedias(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, project, "medias", "", struct {
		Medias        []mediaRowVM
		MaxUploadSize string
	}{s.buildMediaRowVMs(project, medias), humanizeBytes(s.MaxUploadSize)})
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:medias", data)
}

// htmxMediaFile serves a media asset's raw bytes (inline, so <img> previews
// and direct browser viewing both work). Gated the same as the medias list.
func (s *Server) htmxMediaFile(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionMedias)
	if user == nil {
		return
	}
	mediaID := chi.URLParam(r, "mediaID")
	db, err := s.projectDB(project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	m, err := db.GetMedia(r.Context(), mediaID)
	if err != nil {
		writeHTMXStorageError(w, r, err, "/projects/"+project.ID+"/medias")
		return
	}
	path := filepath.Join(s.Manager.ProjectMediaDir(project.ID), m.FilePath)
	w.Header().Set("Content-Type", m.MimeType)
	w.Header().Set("Content-Disposition", `inline; filename="`+m.Filename+`"`)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

func (s *Server) htmxUploadMedia(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionMedias)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/medias"

	r.Body = http.MaxBytesReader(w, r.Body, s.MaxUploadSize)
	if err := r.ParseMultipartForm(multipartMemoryThreshold); err != nil {
		redirectWithFlash(w, r, back, "Fichier trop volumineux ou envoi invalide.", "error")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		redirectWithFlash(w, r, back, "Sélectionnez un fichier à téléverser.", "error")
		return
	}
	defer file.Close()

	id := uuid.NewString()
	storedName := id + filepath.Ext(header.Filename)
	destPath := filepath.Join(s.Manager.ProjectMediaDir(project.ID), storedName)

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		redirectWithFlash(w, r, back, "Impossible d'enregistrer le fichier.", "error")
		return
	}
	written, err := io.Copy(out, file)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		os.Remove(destPath)
		redirectWithFlash(w, r, back, "Impossible d'enregistrer le fichier.", "error")
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	db, err := s.projectDB(project.ID)
	if err != nil {
		os.Remove(destPath)
		redirectWithFlash(w, r, back, "Erreur serveur.", "error")
		return
	}
	m := &storage.Media{ID: id, Filename: header.Filename, MimeType: mimeType, Size: written, FilePath: storedName}
	if err := db.CreateMedia(r.Context(), m); err != nil {
		os.Remove(destPath)
		redirectWithFlash(w, r, back, "Impossible d'enregistrer le média : "+err.Error(), "error")
		return
	}
	redirectWithFlash(w, r, back, "Média « "+header.Filename+" » téléversé.", "success")
}

func (s *Server) htmxDeleteMedia(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionMedias)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/medias"
	mediaID := chi.URLParam(r, "mediaID")

	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, back, "Erreur serveur.", "error")
		return
	}
	m, err := db.GetMedia(r.Context(), mediaID)
	if err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	if err := db.DeleteMedia(r.Context(), mediaID); err != nil {
		redirectWithFlash(w, r, back, "Suppression impossible : "+err.Error(), "error")
		return
	}
	_ = os.Remove(filepath.Join(s.Manager.ProjectMediaDir(project.ID), m.FilePath))
	redirectWithFlash(w, r, back, "Média « "+m.Filename+" » supprimé.", "success")
}
