package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func newTestProjectDB(t *testing.T) *ProjectDB {
	t.Helper()
	db, err := OpenProjectDB(":memory:")
	if err != nil {
		t.Fatalf("open project db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestProjectDB_Folders(t *testing.T) {
	ctx := context.Background()
	db := newTestProjectDB(t)

	root := &Folder{ID: uuid.NewString(), Name: "Root"}
	if err := db.CreateFolder(ctx, root); err != nil {
		t.Fatalf("create root folder: %v", err)
	}
	child := &Folder{ID: uuid.NewString(), Name: "Child", ParentID: &root.ID}
	if err := db.CreateFolder(ctx, child); err != nil {
		t.Fatalf("create child folder: %v", err)
	}

	got, err := db.GetFolder(ctx, child.ID)
	if err != nil || got.ParentID == nil || *got.ParentID != root.ID {
		t.Fatalf("child folder parent mismatch: %v %+v", err, got)
	}

	list, err := db.ListFolders(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 folders: %v (%d)", err, len(list))
	}

	// Deleting root cascades to child (ON DELETE CASCADE).
	if err := db.DeleteFolder(ctx, root.ID); err != nil {
		t.Fatalf("delete root folder: %v", err)
	}
	if _, err := db.GetFolder(ctx, child.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cascade delete of child, got %v", err)
	}
}

func TestProjectDB_Schemas(t *testing.T) {
	ctx := context.Background()
	db := newTestProjectDB(t)

	s := &Schema{Slug: "article", Name: "Article", Definition: `{"fields":[]}`}
	if err := db.CreateSchema(ctx, s); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if err := db.CreateSchema(ctx, s); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists for duplicate slug, got %v", err)
	}

	got, err := db.GetSchema(ctx, "article")
	if err != nil || got.Name != "Article" {
		t.Fatalf("get schema mismatch: %v %+v", err, got)
	}

	// Slug is immutable: UpdateSchema only touches name/folder/definition.
	s.Name = "News Article"
	s.Definition = `{"fields":[{"key":"title","type":"Text","cardinality":"Simple"}]}`
	if err := db.UpdateSchema(ctx, s); err != nil {
		t.Fatalf("update schema: %v", err)
	}
	got2, _ := db.GetSchema(ctx, "article")
	if got2.Name != "News Article" || got2.Slug != "article" {
		t.Fatalf("update mismatch: %+v", got2)
	}

	list, err := db.ListSchemas(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 schema: %v (%d)", err, len(list))
	}

	if err := db.DeleteSchema(ctx, "article"); err != nil {
		t.Fatalf("delete schema: %v", err)
	}
	if _, err := db.GetSchema(ctx, "article"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectDB_Contents(t *testing.T) {
	ctx := context.Background()
	db := newTestProjectDB(t)

	s := &Schema{Slug: "post", Name: "Post", Definition: `{"fields":[]}`}
	if err := db.CreateSchema(ctx, s); err != nil {
		t.Fatal(err)
	}

	c := &Content{ID: uuid.NewString(), SchemaSlug: "post", Data: `{"title":"Hello"}`}
	if err := db.CreateContent(ctx, c); err != nil {
		t.Fatalf("create content: %v", err)
	}
	got, err := db.GetContent(ctx, c.ID)
	if err != nil || got.Status != StatusDraft {
		t.Fatalf("expected default draft status: %v %+v", err, got)
	}

	got.Status = StatusPublished
	got.Data = `{"title":"Hello World"}`
	if err := db.UpdateContent(ctx, got); err != nil {
		t.Fatalf("update content: %v", err)
	}
	got2, _ := db.GetContent(ctx, c.ID)
	if got2.Status != StatusPublished || got2.Data != `{"title":"Hello World"}` {
		t.Fatalf("update not applied: %+v", got2)
	}

	list, err := db.ListContents(ctx, "post")
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 content: %v (%d)", err, len(list))
	}

	n, err := db.CountContentsReferencing(ctx, c.ID)
	if err != nil || n != 0 {
		t.Fatalf("expected 0 references to self by id substring in own doc unrelated: %v (%d)", err, n)
	}

	// Deleting the schema cascades to its contents.
	if err := db.DeleteSchema(ctx, "post"); err != nil {
		t.Fatalf("delete schema: %v", err)
	}
	if _, err := db.GetContent(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected content cascade-deleted, got %v", err)
	}
}

// TestProjectDB_CountContentsBySchema covers the Collections tile grid's
// item-count query: schemas with contents are keyed with their count,
// schemas with none are simply absent from the map (callers default to 0).
func TestProjectDB_CountContentsBySchema(t *testing.T) {
	ctx := context.Background()
	db := newTestProjectDB(t)

	for _, slug := range []string{"post", "author"} {
		if err := db.CreateSchema(ctx, &Schema{Slug: slug, Name: slug, Definition: `{"fields":[]}`}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := db.CreateContent(ctx, &Content{ID: uuid.NewString(), SchemaSlug: "post", Data: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateContent(ctx, &Content{ID: uuid.NewString(), SchemaSlug: "author", Data: `{}`}); err != nil {
		t.Fatal(err)
	}

	counts, err := db.CountContentsBySchema(ctx)
	if err != nil {
		t.Fatalf("count by schema: %v", err)
	}
	if counts["post"] != 3 {
		t.Fatalf("expected 3 for post, got %d", counts["post"])
	}
	if counts["author"] != 1 {
		t.Fatalf("expected 1 for author, got %d", counts["author"])
	}
	// "tag" has no contents at all, and was never created as a schema either
	// -- must simply be absent, not present with 0.
	if _, ok := counts["tag"]; ok {
		t.Fatalf("expected no entry for a schema with zero contents, got %d", counts["tag"])
	}
}

func TestProjectDB_Medias(t *testing.T) {
	ctx := context.Background()
	db := newTestProjectDB(t)

	m := &Media{ID: uuid.NewString(), Filename: "logo.png", MimeType: "image/png", Size: 1024, FilePath: "logo.png"}
	if err := db.CreateMedia(ctx, m); err != nil {
		t.Fatalf("create media: %v", err)
	}

	got, err := db.GetMedia(ctx, m.ID)
	if err != nil || got.Filename != "logo.png" {
		t.Fatalf("get media mismatch: %v %+v", err, got)
	}

	list, err := db.ListMedias(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 media: %v (%d)", err, len(list))
	}

	if err := db.DeleteMedia(ctx, m.ID); err != nil {
		t.Fatalf("delete media: %v", err)
	}
	if _, err := db.GetMedia(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOpenProjectDB_InvalidPath(t *testing.T) {
	if _, err := OpenProjectDB("/nonexistent-dir-xyz/client.db"); err == nil {
		t.Fatal("expected error opening project db at an unwritable path")
	}
}

func TestProjectDB_DBAccessor(t *testing.T) {
	db := newTestProjectDB(t)
	if db.DB() == nil {
		t.Fatal("expected non-nil *sql.DB")
	}
}

func TestProjectDB_DeleteContentExplicit(t *testing.T) {
	ctx := context.Background()
	db := newTestProjectDB(t)
	if err := db.CreateSchema(ctx, &Schema{Slug: "note", Name: "Note", Definition: "{}"}); err != nil {
		t.Fatal(err)
	}
	c := &Content{ID: uuid.NewString(), SchemaSlug: "note", Data: `{}`}
	if err := db.CreateContent(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteContent(ctx, c.ID); err != nil {
		t.Fatalf("delete content: %v", err)
	}
	if _, err := db.GetContent(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := db.DeleteContent(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting already-deleted content, got %v", err)
	}
}
