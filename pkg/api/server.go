package api

import (
	"fmt"
	"html/template"
	"io/fs"
	"sync"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

// Server holds every shared dependency HTTP handlers need. It also owns a
// small cache of open *storage.ProjectDB connections so repeated requests
// against the same project reuse one connection instead of reopening
// client.db on every call.
type Server struct {
	System     *storage.SystemDB
	Manager    *storage.Manager
	Issuer     *auth.TokenIssuer
	Dispatcher *webhooks.Dispatcher
	Templates  *template.Template

	// Encryptor encrypts/decrypts secrets embedded in a webhook's Config
	// (currently only GitHubDispatchConfig.Token). Set post-construction
	// from cmd/tricms/main.go, same pattern as StaticFS/MaxUploadSize below
	// -- nil is fine as long as no github_dispatch webhook is created.
	Encryptor *auth.Encryptor

	// SessionCookieName is the cookie carrying the session JWT.
	SessionCookieName string
	// StaticFS serves /static/* for the HTMX UI (CSS/JS): an embed.FS in
	// production, os.DirFS in tests. Nil is fine when Templates is nil.
	StaticFS fs.FS

	// MaxUploadSize caps a single media upload's total size in bytes.
	// Defaults to defaultMaxUploadSize; overridable (e.g. from an env var)
	// so large-file support doesn't require a code change.
	MaxUploadSize int64

	mu         sync.Mutex
	projectDBs map[string]*storage.ProjectDB
}

// defaultMaxUploadSize is used when Server.MaxUploadSize is left unset.
const defaultMaxUploadSize = 500 << 20 // 500 MiB

// multipartMemoryThreshold caps how much of an upload multipart.Reader keeps
// buffered in RAM; anything beyond this is spilled to a temp file on disk by
// the standard library automatically. Keeping this small (independent of
// MaxUploadSize) is what lets large uploads not blow up server memory.
const multipartMemoryThreshold = 16 << 20 // 16 MiB

// NewServer wires a Server. templates may be nil (HTMX routes will then be
// unavailable, e.g. in tests that only exercise the JSON API).
func NewServer(system *storage.SystemDB, manager *storage.Manager, issuer *auth.TokenIssuer, dispatcher *webhooks.Dispatcher, templates *template.Template) *Server {
	return &Server{
		System:            system,
		Manager:           manager,
		Issuer:            issuer,
		Dispatcher:        dispatcher,
		Templates:         templates,
		SessionCookieName: "tricms_session",
		MaxUploadSize:     defaultMaxUploadSize,
		projectDBs:        make(map[string]*storage.ProjectDB),
	}
}

// projectDB returns a cached, opened ProjectDB for projectID, opening it on
// first use.
func (s *Server) projectDB(projectID string) (*storage.ProjectDB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if db, ok := s.projectDBs[projectID]; ok {
		return db, nil
	}
	db, err := s.Manager.OpenProjectStorage(projectID)
	if err != nil {
		return nil, err
	}
	s.projectDBs[projectID] = db
	return db, nil
}

// forgetProjectDB closes and evicts a project's cached connection, used
// after the project's storage has been deleted.
func (s *Server) forgetProjectDB(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if db, ok := s.projectDBs[projectID]; ok {
		db.Close()
		delete(s.projectDBs, projectID)
	}
}

// Close releases every cached project connection (and the system DB, if
// callers want a single-shot shutdown helper).
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for id, db := range s.projectDBs {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close project %s: %w", id, err)
		}
	}
	s.projectDBs = make(map[string]*storage.ProjectDB)
	return firstErr
}
