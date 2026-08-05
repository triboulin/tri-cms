package storage

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const projectSchema = `
CREATE TABLE IF NOT EXISTS _folders (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    parent_id TEXT REFERENCES _folders(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS _schemas (
    slug TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    folder_id TEXT REFERENCES _folders(id) ON DELETE SET NULL,
    definition JSON NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS _contents (
    id TEXT PRIMARY KEY,
    schema_slug TEXT REFERENCES _schemas(slug) ON DELETE CASCADE,
    data JSON NOT NULL,
    status TEXT CHECK(status IN ('draft', 'published')) DEFAULT 'draft',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS _medias (
    id TEXT PRIMARY KEY,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// ProjectDB wraps a single project's client.db connection.
type ProjectDB struct {
	db *sql.DB
}

// OpenProjectDB opens (and migrates) a project database.
// Use dsn ":memory:" for an isolated in-memory instance (tests).
func OpenProjectDB(dsn string) (*ProjectDB, error) {
	db, err := sql.Open("sqlite", dsnFor(dsn))
	if err != nil {
		return nil, fmt.Errorf("open project db: %w", err)
	}
	if dsn == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(projectSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate project db: %w", err)
	}
	return &ProjectDB{db: db}, nil
}

func (p *ProjectDB) Close() error { return p.db.Close() }
func (p *ProjectDB) DB() *sql.DB  { return p.db }

// ---- Folders ----

func (p *ProjectDB) CreateFolder(ctx context.Context, f *Folder) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO _folders (id, name, parent_id) VALUES (?, ?, ?)`, f.ID, f.Name, f.ParentID)
	return err
}

func (p *ProjectDB) GetFolder(ctx context.Context, id string) (*Folder, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT id, name, parent_id, created_at FROM _folders WHERE id = ?`, id)
	var f Folder
	if err := row.Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (p *ProjectDB) ListFolders(ctx context.Context) ([]*Folder, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT id, name, parent_id, created_at FROM _folders ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (p *ProjectDB) DeleteFolder(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM _folders WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- Schemas ----

func (p *ProjectDB) CreateSchema(ctx context.Context, s *Schema) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO _schemas (slug, name, folder_id, definition) VALUES (?, ?, ?, ?)`,
		s.Slug, s.Name, s.FolderID, s.Definition)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (p *ProjectDB) GetSchema(ctx context.Context, slug string) (*Schema, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT slug, name, folder_id, definition, created_at, updated_at FROM _schemas WHERE slug = ?`, slug)
	var s Schema
	if err := row.Scan(&s.Slug, &s.Name, &s.FolderID, &s.Definition, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (p *ProjectDB) ListSchemas(ctx context.Context) ([]*Schema, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT slug, name, folder_id, definition, created_at, updated_at FROM _schemas ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schema
	for rows.Next() {
		var s Schema
		if err := rows.Scan(&s.Slug, &s.Name, &s.FolderID, &s.Definition, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// UpdateSchema updates name/folder/definition but never the slug (immutable per spec).
func (p *ProjectDB) UpdateSchema(ctx context.Context, s *Schema) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE _schemas SET name = ?, folder_id = ?, definition = ?, updated_at = CURRENT_TIMESTAMP WHERE slug = ?`,
		s.Name, s.FolderID, s.Definition, s.Slug)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (p *ProjectDB) DeleteSchema(ctx context.Context, slug string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM _schemas WHERE slug = ?`, slug)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- Contents ----

func (p *ProjectDB) CreateContent(ctx context.Context, c *Content) error {
	if c.Status == "" {
		c.Status = StatusDraft
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO _contents (id, schema_slug, data, status) VALUES (?, ?, ?, ?)`,
		c.ID, c.SchemaSlug, c.Data, string(c.Status))
	return err
}

func (p *ProjectDB) GetContent(ctx context.Context, id string) (*Content, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT id, schema_slug, data, status, created_at, updated_at FROM _contents WHERE id = ?`, id)
	var c Content
	var status string
	if err := row.Scan(&c.ID, &c.SchemaSlug, &c.Data, &status, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Status = ContentStatus(status)
	return &c, nil
}

func (p *ProjectDB) ListContents(ctx context.Context, schemaSlug string) ([]*Content, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, schema_slug, data, status, created_at, updated_at FROM _contents WHERE schema_slug = ? ORDER BY created_at DESC`,
		schemaSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Content
	for rows.Next() {
		var c Content
		var status string
		if err := rows.Scan(&c.ID, &c.SchemaSlug, &c.Data, &status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Status = ContentStatus(status)
		out = append(out, &c)
	}
	return out, rows.Err()
}

// CountContentsReferencing counts contents whose data contains the given id
// as the value of a Reference-type field (naive substring pre-check; final
// authority is pkg/schema which knows field types).
func (p *ProjectDB) CountContentsReferencing(ctx context.Context, contentID string) (int, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM _contents WHERE data LIKE '%' || ? || '%'`, contentID)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (p *ProjectDB) UpdateContent(ctx context.Context, c *Content) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE _contents SET data = ?, status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		c.Data, string(c.Status), c.ID)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (p *ProjectDB) DeleteContent(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM _contents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- Medias ----

func (p *ProjectDB) CreateMedia(ctx context.Context, m *Media) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO _medias (id, filename, mime_type, size, file_path) VALUES (?, ?, ?, ?, ?)`,
		m.ID, m.Filename, m.MimeType, m.Size, m.FilePath)
	return err
}

func (p *ProjectDB) GetMedia(ctx context.Context, id string) (*Media, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT id, filename, mime_type, size, file_path, created_at, updated_at FROM _medias WHERE id = ?`, id)
	var m Media
	if err := row.Scan(&m.ID, &m.Filename, &m.MimeType, &m.Size, &m.FilePath, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

func (p *ProjectDB) ListMedias(ctx context.Context) ([]*Media, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, filename, mime_type, size, file_path, created_at, updated_at FROM _medias ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.Filename, &m.MimeType, &m.Size, &m.FilePath, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (p *ProjectDB) DeleteMedia(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM _medias WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}
