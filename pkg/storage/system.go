package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const systemSchema = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_global_admin BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    folder_path TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS project_permissions (
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    role TEXT CHECK(role IN ('CONCEPTEUR', 'GESTIONNAIRE', 'REDACTEUR')) NOT NULL,
    PRIMARY KEY (user_id, project_id)
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS webhooks (
    id TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'generic',
    url TEXT NOT NULL DEFAULT '',
    secret TEXT NOT NULL DEFAULT '',
    config TEXT,
    events TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS global_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT,
    project_id TEXT,
    action TEXT NOT NULL,
    details JSON,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// SystemDB wraps the global system.db connection.
type SystemDB struct {
	db *sql.DB
}

// OpenSystemDB opens (and migrates) the global system database.
// Use dsn ":memory:" for an isolated in-memory instance (tests),
// or a file path for persistent storage.
func OpenSystemDB(dsn string) (*SystemDB, error) {
	db, err := sql.Open("sqlite", dsnFor(dsn))
	if err != nil {
		return nil, fmt.Errorf("open system db: %w", err)
	}
	if dsn == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(systemSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate system db: %w", err)
	}
	if err := migrateWebhookColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate system db: %w", err)
	}
	return &SystemDB{db: db}, nil
}

// migrateWebhookColumns adds the kind/config columns to pre-existing
// webhooks tables (from before the github_dispatch webhook kind existed).
// CREATE TABLE IF NOT EXISTS above only applies to brand-new databases, so
// this ALTER TABLE step keeps already-deployed system.db files working
// without a manual migration step. Safe to run on every startup: ALTER
// TABLE ADD COLUMN fails harmlessly with "duplicate column name" once the
// columns already exist.
func migrateWebhookColumns(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE webhooks ADD COLUMN kind TEXT NOT NULL DEFAULT 'generic'`,
		`ALTER TABLE webhooks ADD COLUMN config TEXT`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

func dsnFor(dsn string) string {
	if dsn == ":memory:" {
		return "file::memory:?cache=shared"
	}
	return dsn
}

func (s *SystemDB) Close() error { return s.db.Close() }

// DB exposes the underlying *sql.DB for advanced use (rarely needed).
func (s *SystemDB) DB() *sql.DB { return s.db }

// ---- Users ----

func (s *SystemDB) CreateUser(ctx context.Context, u *User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, is_global_admin) VALUES (?, ?, ?, ?)`,
		u.ID, strings.ToLower(u.Email), u.PasswordHash, u.IsGlobalAdmin,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *SystemDB) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, is_global_admin, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *SystemDB) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, is_global_admin, created_at FROM users WHERE email = ?`,
		strings.ToLower(email))
	return scanUser(row)
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsGlobalAdmin, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (s *SystemDB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, password_hash, is_global_admin, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsGlobalAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

func (s *SystemDB) UpdateUserPassword(ctx context.Context, id, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SystemDB) SetGlobalAdmin(ctx context.Context, id string, isAdmin bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET is_global_admin = ? WHERE id = ?`, isAdmin, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SystemDB) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- Projects ----

func (s *SystemDB) CreateProject(ctx context.Context, p *Project) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, folder_path) VALUES (?, ?, ?)`,
		p.ID, p.Name, p.FolderPath)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *SystemDB) GetProject(ctx context.Context, id string) (*Project, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, folder_path, created_at FROM projects WHERE id = ?`, id)
	var p Project
	if err := row.Scan(&p.ID, &p.Name, &p.FolderPath, &p.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (s *SystemDB) ListProjects(ctx context.Context) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, folder_path, created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.FolderPath, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ListProjectsForUser returns all projects a user can access: every project if
// the user is a global admin, otherwise only projects with a permission row.
func (s *SystemDB) ListProjectsForUser(ctx context.Context, userID string, isGlobalAdmin bool) ([]*Project, error) {
	if isGlobalAdmin {
		return s.ListProjects(ctx)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.folder_path, p.created_at
		FROM projects p
		JOIN project_permissions pp ON pp.project_id = p.id
		WHERE pp.user_id = ?
		ORDER BY p.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.FolderPath, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *SystemDB) DeleteProject(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- Project Permissions ----

func (s *SystemDB) SetProjectPermission(ctx context.Context, pp *ProjectPermission) error {
	if !pp.Role.Valid() {
		return fmt.Errorf("%w: invalid role %q", ErrConflict, pp.Role)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO project_permissions (user_id, project_id, role) VALUES (?, ?, ?)
		ON CONFLICT(user_id, project_id) DO UPDATE SET role = excluded.role`,
		pp.UserID, pp.ProjectID, string(pp.Role))
	return err
}

func (s *SystemDB) GetProjectPermission(ctx context.Context, userID, projectID string) (*ProjectPermission, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT user_id, project_id, role FROM project_permissions WHERE user_id = ? AND project_id = ?`,
		userID, projectID)
	var pp ProjectPermission
	var role string
	if err := row.Scan(&pp.UserID, &pp.ProjectID, &role); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	pp.Role = Role(role)
	return &pp, nil
}

func (s *SystemDB) ListProjectPermissions(ctx context.Context, projectID string) ([]*ProjectPermission, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, project_id, role FROM project_permissions WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProjectPermission
	for rows.Next() {
		var pp ProjectPermission
		var role string
		if err := rows.Scan(&pp.UserID, &pp.ProjectID, &role); err != nil {
			return nil, err
		}
		pp.Role = Role(role)
		out = append(out, &pp)
	}
	return out, rows.Err()
}

func (s *SystemDB) DeleteProjectPermission(ctx context.Context, userID, projectID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM project_permissions WHERE user_id = ? AND project_id = ?`, userID, projectID)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- API Tokens ----

func (s *SystemDB) CreateAPIToken(ctx context.Context, t *APIToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (id, project_id, token_hash, name) VALUES (?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.TokenHash, t.Name)
	return err
}

func (s *SystemDB) GetAPITokenByHash(ctx context.Context, hash string) (*APIToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, token_hash, name, created_at FROM api_tokens WHERE token_hash = ?`, hash)
	var t APIToken
	if err := row.Scan(&t.ID, &t.ProjectID, &t.TokenHash, &t.Name, &t.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *SystemDB) ListAPITokens(ctx context.Context, projectID string) ([]*APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, token_hash, name, created_at FROM api_tokens WHERE project_id = ? ORDER BY created_at`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.TokenHash, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (s *SystemDB) DeleteAPIToken(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- Webhooks ----

func (s *SystemDB) CreateWebhook(ctx context.Context, w *Webhook) error {
	events, err := json.Marshal(w.Events)
	if err != nil {
		return err
	}
	if w.Kind == "" {
		w.Kind = "generic"
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO webhooks (id, project_id, kind, url, secret, config, events) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.ProjectID, w.Kind, w.URL, w.Secret, nullable(w.Config), string(events))
	return err
}

func (s *SystemDB) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, kind, url, secret, COALESCE(config, ''), events, created_at FROM webhooks WHERE id = ?`, id)
	return scanWebhook(row)
}

func scanWebhook(row *sql.Row) (*Webhook, error) {
	var w Webhook
	var events string
	if err := row.Scan(&w.ID, &w.ProjectID, &w.Kind, &w.URL, &w.Secret, &w.Config, &events, &w.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(events), &w.Events)
	return &w, nil
}

func (s *SystemDB) ListWebhooks(ctx context.Context, projectID string) ([]*Webhook, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, kind, url, secret, COALESCE(config, ''), events, created_at FROM webhooks WHERE project_id = ? ORDER BY created_at`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Webhook
	for rows.Next() {
		var w Webhook
		var events string
		if err := rows.Scan(&w.ID, &w.ProjectID, &w.Kind, &w.URL, &w.Secret, &w.Config, &events, &w.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(events), &w.Events)
		out = append(out, &w)
	}
	return out, rows.Err()
}

// ListWebhooksForEvent returns all webhooks of a project subscribed to a given event.
func (s *SystemDB) ListWebhooksForEvent(ctx context.Context, projectID, event string) ([]*Webhook, error) {
	all, err := s.ListWebhooks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var out []*Webhook
	for _, w := range all {
		for _, e := range w.Events {
			if e == event {
				out = append(out, w)
				break
			}
		}
	}
	return out, nil
}

func (s *SystemDB) UpdateWebhook(ctx context.Context, w *Webhook) error {
	events, err := json.Marshal(w.Events)
	if err != nil {
		return err
	}
	if w.Kind == "" {
		w.Kind = "generic"
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET kind = ?, url = ?, secret = ?, config = ?, events = ? WHERE id = ?`,
		w.Kind, w.URL, w.Secret, nullable(w.Config), string(events), w.ID)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SystemDB) DeleteWebhook(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

// ---- Global Logs ----

func (s *SystemDB) LogAction(ctx context.Context, userID, projectID, action string, details any) error {
	var detailsJSON []byte
	if details != nil {
		var err error
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO global_logs (user_id, project_id, action, details) VALUES (?, ?, ?, ?)`,
		nullable(userID), nullable(projectID), action, nullableBytes(detailsJSON))
	return err
}

func (s *SystemDB) ListLogs(ctx context.Context, limit int) ([]*GlobalLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(user_id, ''), COALESCE(project_id, ''), action, COALESCE(details, ''), created_at
		 FROM global_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*GlobalLog
	for rows.Next() {
		var l GlobalLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.ProjectID, &l.Action, &l.Details, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// ListLogsByProject returns the audit trail for a single project (most
// recent first), instead of the whole installation -- the audit log view is
// project-scoped (still admin-only), not a cross-project firehose.
func (s *SystemDB) ListLogsByProject(ctx context.Context, projectID string, limit int) ([]*GlobalLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(user_id, ''), COALESCE(project_id, ''), action, COALESCE(details, ''), created_at
		 FROM global_logs WHERE project_id = ? ORDER BY id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*GlobalLog
	for rows.Next() {
		var l GlobalLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.ProjectID, &l.Action, &l.Details, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// ---- helpers ----

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func checkRowsAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

var _ = time.Now
