package storage

import "time"

// Role represents a project-scoped role. Roles are cumulative:
// REDACTEUR ⊂ GESTIONNAIRE ⊂ CONCEPTEUR.
type Role string

const (
	RoleConcepteur   Role = "CONCEPTEUR"
	RoleGestionnaire Role = "GESTIONNAIRE"
	RoleRedacteur    Role = "REDACTEUR"
)

// Level returns a numeric rank for role hierarchy comparisons.
// Higher is more privileged.
func (r Role) Level() int {
	switch r {
	case RoleConcepteur:
		return 3
	case RoleGestionnaire:
		return 2
	case RoleRedacteur:
		return 1
	default:
		return 0
	}
}

func (r Role) Valid() bool {
	switch r {
	case RoleConcepteur, RoleGestionnaire, RoleRedacteur:
		return true
	}
	return false
}

type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	PasswordHash  string    `json:"-"`
	IsGlobalAdmin bool      `json:"is_global_admin"`
	CreatedAt     time.Time `json:"created_at"`
}

type Project struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	FolderPath string    `json:"folder_path"`
	CreatedAt  time.Time `json:"created_at"`
}

type ProjectPermission struct {
	UserID    string `json:"user_id"`
	ProjectID string `json:"project_id"`
	Role      Role   `json:"role"`
}

// APIToken is now always a global, ADMIN-equivalent credential (see
// pkg/auth middleware): ProjectID is retained only for backward
// compatibility with rows created before this change and is always nil
// for tokens minted going forward. Tokens are managed exclusively from the
// Administration area, not from a project's own settings.
type APIToken struct {
	ID        string    `json:"id"`
	ProjectID *string   `json:"project_id,omitempty"`
	TokenHash string    `json:"-"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Webhook struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Kind      string    `json:"kind"` // "generic" (default) or "github_dispatch"
	URL       string    `json:"url"`  // used by kind=generic
	Secret    string    `json:"-"`    // HMAC secret, used by kind=generic
	Config    string    `json:"-"`    // raw JSON, kind-specific (e.g. github_dispatch owner/repo/token); token is encrypted at rest, never serialized as-is -- see pkg/api masking
	Events    []string  `json:"events"`
	CreatedAt time.Time `json:"created_at"`
}

type GlobalLog struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"user_id"`
	ProjectID string    `json:"project_id"`
	Action    string    `json:"action"`
	Details   string    `json:"details"` // raw JSON
	CreatedAt time.Time `json:"created_at"`
}

type Folder struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  *string   `json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`
}

type Schema struct {
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	FolderID   *string   `json:"folder_id"`
	Definition string    `json:"definition"` // raw JSON
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ContentStatus string

const (
	StatusDraft     ContentStatus = "draft"
	StatusPublished ContentStatus = "published"
)

type Content struct {
	ID         string        `json:"id"`
	SchemaSlug string        `json:"schema_slug"`
	Data       string        `json:"data"` // raw JSON
	Status     ContentStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type Media struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	Size      int64     `json:"size"`
	FilePath  string    `json:"file_path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
