package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestManager_CreateOpenDeleteProjectStorage(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base)

	projectID := "proj_" + uuid.NewString()

	db, err := m.CreateProjectStorage(projectID)
	if err != nil {
		t.Fatalf("create project storage: %v", err)
	}

	// The client.db file and media directory must exist on disk.
	if _, err := os.Stat(m.ProjectDBPath(projectID)); err != nil {
		t.Fatalf("expected client.db to exist: %v", err)
	}
	if _, err := os.Stat(m.ProjectMediaDir(projectID)); err != nil {
		t.Fatalf("expected media dir to exist: %v", err)
	}

	// Isolation: writing to this project's db must not appear in a sibling project's db.
	ctx := context.Background()
	if err := db.CreateSchema(ctx, &Schema{Slug: "s1", Name: "S1", Definition: "{}"}); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db.Close()

	otherID := "proj_" + uuid.NewString()
	otherDB, err := m.CreateProjectStorage(otherID)
	if err != nil {
		t.Fatalf("create other project storage: %v", err)
	}
	if _, err := otherDB.GetSchema(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected project isolation, but schema leaked: %v", err)
	}
	otherDB.Close()

	// Re-creating over an existing project directory must fail.
	if _, err := m.CreateProjectStorage(projectID); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	// Opening existing storage should retain previously written data.
	reopened, err := m.OpenProjectStorage(projectID)
	if err != nil {
		t.Fatalf("reopen project storage: %v", err)
	}
	if _, err := reopened.GetSchema(ctx, "s1"); err != nil {
		t.Fatalf("expected schema to persist across reopen: %v", err)
	}
	reopened.Close()

	// Opening storage for a project that never existed fails.
	if _, err := m.OpenProjectStorage("proj_does_not_exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := m.DeleteProjectStorage(projectID); err != nil {
		t.Fatalf("delete project storage: %v", err)
	}
	if _, err := os.Stat(m.ProjectDir(projectID)); !os.IsNotExist(err) {
		t.Fatalf("expected project directory removed")
	}
	if err := m.DeleteProjectStorage(projectID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}

	_ = filepath.Join // keep import used if refactored
	m.DeleteProjectStorage(otherID)
}
