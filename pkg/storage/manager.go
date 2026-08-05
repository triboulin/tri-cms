package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// Manager resolves and opens per-project databases/media directories under
// a base data directory, enforcing the physical multi-tenant isolation
// described in the spec: ./data/projects/{project_id}/client.db.
type Manager struct {
	BaseDir string // e.g. "./data"
}

func NewManager(baseDir string) *Manager {
	return &Manager{BaseDir: baseDir}
}

func (m *Manager) ProjectDir(projectID string) string {
	return filepath.Join(m.BaseDir, "projects", projectID)
}

func (m *Manager) ProjectDBPath(projectID string) string {
	return filepath.Join(m.ProjectDir(projectID), "client.db")
}

func (m *Manager) ProjectMediaDir(projectID string) string {
	return filepath.Join(m.ProjectDir(projectID), "media")
}

// CreateProjectStorage creates the on-disk folder structure for a new
// project and returns an opened, migrated ProjectDB.
func (m *Manager) CreateProjectStorage(projectID string) (*ProjectDB, error) {
	dir := m.ProjectDir(projectID)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%w: project directory already exists", ErrAlreadyExists)
	}
	if err := os.MkdirAll(m.ProjectMediaDir(projectID), 0o750); err != nil {
		return nil, fmt.Errorf("create project directory: %w", err)
	}
	db, err := OpenProjectDB(m.ProjectDBPath(projectID))
	if err != nil {
		return nil, err
	}
	return db, nil
}

// OpenProjectStorage opens the database of an already-existing project.
func (m *Manager) OpenProjectStorage(projectID string) (*ProjectDB, error) {
	path := m.ProjectDBPath(projectID)
	if _, err := os.Stat(path); err != nil {
		return nil, ErrNotFound
	}
	return OpenProjectDB(path)
}

// DeleteProjectStorage irreversibly removes a project's directory
// (client.db + media). Callers must have already enforced double
// confirmation at the API/UI layer.
func (m *Manager) DeleteProjectStorage(projectID string) error {
	dir := m.ProjectDir(projectID)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return os.RemoveAll(dir)
}
