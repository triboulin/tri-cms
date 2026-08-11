package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestSystemDB(t *testing.T) *SystemDB {
	t.Helper()
	db, err := OpenSystemDB(":memory:")
	if err != nil {
		t.Fatalf("open system db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSystemDB_UserCRUD(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)

	u := &User{ID: uuid.NewString(), Email: "Admin@Example.com", PasswordHash: "hash1", IsGlobalAdmin: true}
	if err := db.CreateUser(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Duplicate email must fail.
	dup := &User{ID: uuid.NewString(), Email: "admin@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, dup); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	// Emails are stored/matched case-insensitively.
	got, err := db.GetUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("expected user %s, got %s", u.ID, got.ID)
	}

	got2, err := db.GetUserByID(ctx, u.ID)
	if err != nil || got2.Email != "admin@example.com" {
		t.Fatalf("get by id mismatch: %v %+v", err, got2)
	}

	if _, err := db.GetUserByID(ctx, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := db.UpdateUserPassword(ctx, u.ID, "newhash"); err != nil {
		t.Fatalf("update password: %v", err)
	}
	got3, _ := db.GetUserByID(ctx, u.ID)
	if got3.PasswordHash != "newhash" {
		t.Fatalf("password not updated")
	}

	if err := db.SetGlobalAdmin(ctx, u.ID, false); err != nil {
		t.Fatalf("set global admin: %v", err)
	}
	got4, _ := db.GetUserByID(ctx, u.ID)
	if got4.IsGlobalAdmin {
		t.Fatalf("expected is_global_admin=false")
	}

	list, err := db.ListUsers(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 user, got %d (%v)", len(list), err)
	}

	if err := db.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if err := db.DeleteUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
}

func TestSystemDB_ProjectsAndPermissions(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)

	admin := &User{ID: uuid.NewString(), Email: "a@x.com", PasswordHash: "h", IsGlobalAdmin: true}
	editor := &User{ID: uuid.NewString(), Email: "e@x.com", PasswordHash: "h"}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(ctx, editor); err != nil {
		t.Fatal(err)
	}

	proj := &Project{ID: "proj_test", Name: "Test", FolderPath: "./data/projects/proj_test"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.CreateProject(ctx, proj); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	got, err := db.GetProject(ctx, proj.ID)
	if err != nil || got.Name != "Test" {
		t.Fatalf("get project mismatch: %v %+v", err, got)
	}

	// Global admin sees all projects without explicit permission rows.
	adminProjects, err := db.ListProjectsForUser(ctx, admin.ID, true)
	if err != nil || len(adminProjects) != 1 {
		t.Fatalf("admin should see project: %v (%d)", err, len(adminProjects))
	}

	// Regular user without permission sees nothing.
	editorProjects, err := db.ListProjectsForUser(ctx, editor.ID, false)
	if err != nil || len(editorProjects) != 0 {
		t.Fatalf("editor should see no projects yet: %v (%d)", err, len(editorProjects))
	}

	if err := db.SetProjectPermission(ctx, &ProjectPermission{UserID: editor.ID, ProjectID: proj.ID, Role: RoleRedacteur}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	editorProjects, err = db.ListProjectsForUser(ctx, editor.ID, false)
	if err != nil || len(editorProjects) != 1 {
		t.Fatalf("editor should now see 1 project: %v (%d)", err, len(editorProjects))
	}

	pp, err := db.GetProjectPermission(ctx, editor.ID, proj.ID)
	if err != nil || pp.Role != RoleRedacteur {
		t.Fatalf("expected REDACTEUR role: %v %+v", err, pp)
	}

	// Upsert: changing role for same (user,project) pair.
	if err := db.SetProjectPermission(ctx, &ProjectPermission{UserID: editor.ID, ProjectID: proj.ID, Role: RoleGestionnaire}); err != nil {
		t.Fatalf("upsert permission: %v", err)
	}
	pp2, _ := db.GetProjectPermission(ctx, editor.ID, proj.ID)
	if pp2.Role != RoleGestionnaire {
		t.Fatalf("expected role upgraded to GESTIONNAIRE, got %s", pp2.Role)
	}

	if err := db.SetProjectPermission(ctx, &ProjectPermission{UserID: editor.ID, ProjectID: proj.ID, Role: "BOGUS"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for invalid role, got %v", err)
	}

	perms, err := db.ListProjectPermissions(ctx, proj.ID)
	if err != nil || len(perms) != 1 {
		t.Fatalf("expected 1 permission row: %v (%d)", err, len(perms))
	}

	if err := db.DeleteProjectPermission(ctx, editor.ID, proj.ID); err != nil {
		t.Fatalf("delete permission: %v", err)
	}
	if _, err := db.GetProjectPermission(ctx, editor.ID, proj.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	if err := db.DeleteProject(ctx, proj.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := db.GetProject(ctx, proj.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after project delete, got %v", err)
	}
}

func TestRole_Level(t *testing.T) {
	cases := []struct {
		role Role
		want int
	}{
		{RoleRedacteur, 1},
		{RoleGestionnaire, 2},
		{RoleConcepteur, 3},
		{Role("unknown"), 0},
	}
	for _, c := range cases {
		if got := c.role.Level(); got != c.want {
			t.Errorf("Level(%s) = %d, want %d", c.role, got, c.want)
		}
	}
	if !RoleRedacteur.Valid() || !RoleGestionnaire.Valid() || !RoleConcepteur.Valid() {
		t.Fatal("expected known roles to be valid")
	}
	if Role("nope").Valid() {
		t.Fatal("expected unknown role to be invalid")
	}
	if RoleGestionnaire.Level() <= RoleRedacteur.Level() {
		t.Fatal("expected GESTIONNAIRE to outrank REDACTEUR")
	}
	if RoleConcepteur.Level() <= RoleGestionnaire.Level() {
		t.Fatal("expected CONCEPTEUR to outrank GESTIONNAIRE")
	}
}

func TestSystemDB_APITokens(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_tok", Name: "Tok", FolderPath: "./data/projects/proj_tok"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	// Tokens are global/ADMIN-equivalent now (see pkg/api middleware):
	// ProjectID is left nil for every token minted going forward, and
	// ListAPITokens no longer takes a project scope.
	tok := &APIToken{ID: uuid.NewString(), TokenHash: "hashed", Name: "CI token"}
	if err := db.CreateAPIToken(ctx, tok); err != nil {
		t.Fatalf("create token: %v", err)
	}

	got, err := db.GetAPITokenByHash(ctx, "hashed")
	if err != nil || got.ID != tok.ID {
		t.Fatalf("get by hash mismatch: %v %+v", err, got)
	}
	if got.ProjectID != nil {
		t.Fatalf("expected nil ProjectID for a global token, got %v", *got.ProjectID)
	}

	list, err := db.ListAPITokens(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 token: %v (%d)", err, len(list))
	}

	if err := db.DeleteAPIToken(ctx, tok.ID); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	if _, err := db.GetAPITokenByHash(ctx, "hashed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSystemDB_Webhooks(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_wh", Name: "WH", FolderPath: "./data/projects/proj_wh"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	wh := &Webhook{ID: uuid.NewString(), ProjectID: proj.ID, URL: "https://example.com/hook", Secret: "s3cr3t", Events: []string{"content.create", "content.update"}}
	if err := db.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	got, err := db.GetWebhook(ctx, wh.ID)
	if err != nil || len(got.Events) != 2 {
		t.Fatalf("get webhook mismatch: %v %+v", err, got)
	}

	matches, err := db.ListWebhooksForEvent(ctx, proj.ID, "content.create")
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected 1 match for content.create: %v (%d)", err, len(matches))
	}
	none, err := db.ListWebhooksForEvent(ctx, proj.ID, "content.delete")
	if err != nil || len(none) != 0 {
		t.Fatalf("expected 0 matches for content.delete: %v (%d)", err, len(none))
	}

	wh.URL = "https://example.com/hook2"
	wh.Events = []string{"content.delete"}
	if err := db.UpdateWebhook(ctx, wh); err != nil {
		t.Fatalf("update webhook: %v", err)
	}
	got2, _ := db.GetWebhook(ctx, wh.ID)
	if got2.URL != "https://example.com/hook2" || len(got2.Events) != 1 {
		t.Fatalf("update not applied: %+v", got2)
	}

	list, err := db.ListWebhooks(ctx, proj.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 webhook: %v (%d)", err, len(list))
	}

	if err := db.DeleteWebhook(ctx, wh.ID); err != nil {
		t.Fatalf("delete webhook: %v", err)
	}
	if _, err := db.GetWebhook(ctx, wh.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestSystemDB_MigrateWebhookEventNames is a regression test for the
// content.* event collapse (see pkg/webhooks.EventContentUpdate's doc
// comment): a webhook provisioned back when granular events existed --
// e.g. neon-blog's, created with events: ["content.publish",
// "content.unpublish"] -- must have those rewritten to content.update on
// startup, or it would silently stop matching anything once the dispatch
// side only ever sends content.update.
func TestSystemDB_MigrateWebhookEventNames(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_mig", Name: "Migrate", FolderPath: "./data/projects/proj_mig"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	// Simulate pre-existing rows written before the collapse, bypassing
	// CreateWebhook (which would encode today's Go constants, not the old
	// on-disk values this migration exists to fix).
	rawRows := []struct {
		id     string
		events string
	}{
		{uuid.NewString(), `["content.publish","content.unpublish"]`},
		{uuid.NewString(), `["content.create","content.update"]`},
		{uuid.NewString(), `["content.delete"]`},
		{uuid.NewString(), `["content.update","media.create"]`},
	}
	for _, r := range rawRows {
		if _, err := db.db.ExecContext(ctx,
			`INSERT INTO webhooks (id, project_id, url, secret, events) VALUES (?, ?, ?, ?, ?)`,
			r.id, proj.ID, "https://example.com/hook", "s", r.events); err != nil {
			t.Fatalf("seed raw webhook row: %v", err)
		}
	}

	if err := migrateWebhookEventNames(db.db); err != nil {
		t.Fatalf("migrateWebhookEventNames: %v", err)
	}

	for _, r := range rawRows {
		got, err := db.GetWebhook(ctx, r.id)
		if err != nil {
			t.Fatalf("get webhook %s: %v", r.id, err)
		}
		seen := make(map[string]bool)
		for _, ev := range got.Events {
			if ev != "content.update" && ev != "media.create" && ev != "media.delete" {
				t.Fatalf("webhook %s still has stale event %q after migration: %v", r.id, ev, got.Events)
			}
			if seen[ev] {
				t.Fatalf("webhook %s has duplicate event %q after migration: %v", r.id, ev, got.Events)
			}
			seen[ev] = true
		}
	}

	// Running the migration again must be a no-op (idempotent).
	if err := migrateWebhookEventNames(db.db); err != nil {
		t.Fatalf("second migrateWebhookEventNames run: %v", err)
	}
}

func TestSystemDB_WebhookDeliveries(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_whd", Name: "WHD", FolderPath: "./data/projects/proj_whd"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	ok := &WebhookDelivery{WebhookID: "wh1", ProjectID: proj.ID, Event: "content.publish", Success: true, Attempts: 1, StatusCode: 200}
	if err := db.RecordWebhookDelivery(ctx, ok); err != nil {
		t.Fatalf("record success: %v", err)
	}
	failed := &WebhookDelivery{WebhookID: "wh1", ProjectID: proj.ID, Event: "content.publish", Success: false, Attempts: 5, StatusCode: 500, Error: "unexpected status code 500"}
	if err := db.RecordWebhookDelivery(ctx, failed); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	deliveries, err := db.ListWebhookDeliveries(ctx, proj.ID, 10)
	if err != nil || len(deliveries) != 2 {
		t.Fatalf("expected 2 deliveries: %v (%d)", err, len(deliveries))
	}
	// Newest first.
	if deliveries[0].Success || deliveries[0].Error == "" {
		t.Fatalf("expected the failed delivery first (most recent): %+v", deliveries[0])
	}
	if !deliveries[1].Success || deliveries[1].StatusCode != 200 {
		t.Fatalf("expected the successful delivery second: %+v", deliveries[1])
	}

	limited, err := db.ListWebhookDeliveries(ctx, proj.ID, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("expected limit to be honored: %v (%d)", err, len(limited))
	}

	other, err := db.ListWebhookDeliveries(ctx, "some-other-project", 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("expected deliveries scoped to their project: %v (%d)", err, len(other))
	}
}

func TestSystemDB_WebhookDelivery_DeployState(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_whds", Name: "WHDS", FolderPath: "./data/projects/proj_whds"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}
	d := &WebhookDelivery{WebhookID: "wh1", ProjectID: proj.ID, Event: "content.publish", Success: true, Attempts: 1, StatusCode: 202}
	if err := db.RecordWebhookDelivery(ctx, d); err != nil {
		t.Fatalf("record: %v", err)
	}

	deliveries, err := db.ListWebhookDeliveries(ctx, proj.ID, 10)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery: %v (%d)", err, len(deliveries))
	}
	got := deliveries[0]
	if got.DeployRunID != 0 || got.DeployStatus != "" || got.DeployConclusion != "" || got.DeployRunURL != "" {
		t.Fatalf("expected empty deploy state by default: %+v", got)
	}
	if got.DeployFinal() {
		t.Fatal("empty deploy status must not be considered final")
	}

	if err := db.UpdateWebhookDeliveryDeployState(ctx, got.ID, 12345, "in_progress", "", "https://github.com/o/r/actions/runs/12345"); err != nil {
		t.Fatalf("update to in_progress: %v", err)
	}
	deliveries, _ = db.ListWebhookDeliveries(ctx, proj.ID, 10)
	got = deliveries[0]
	if got.DeployRunID != 12345 || got.DeployStatus != "in_progress" || got.DeployRunURL == "" {
		t.Fatalf("expected in_progress deploy state: %+v", got)
	}
	if got.DeployFinal() {
		t.Fatal("in_progress must not be considered final")
	}

	if err := db.UpdateWebhookDeliveryDeployState(ctx, got.ID, 12345, "completed", "success", got.DeployRunURL); err != nil {
		t.Fatalf("update to completed: %v", err)
	}
	deliveries, _ = db.ListWebhookDeliveries(ctx, proj.ID, 10)
	got = deliveries[0]
	if got.DeployStatus != "completed" || got.DeployConclusion != "success" {
		t.Fatalf("expected completed/success deploy state: %+v", got)
	}
	if !got.DeployFinal() {
		t.Fatal("completed must be considered final")
	}
	if got.DeployResolvedAt == nil {
		t.Fatal("expected deploy_resolved_at to be stamped on first transition to completed")
	}
	if !got.DeployRecentlyResolved(time.Minute) {
		t.Fatal("expected a just-resolved delivery to be within a 1-minute grace window")
	}
	if got.DeployRecentlyResolved(0) {
		t.Fatal("a zero-duration grace window must never match")
	}

	if err := db.UpdateWebhookDeliveryDeployState(ctx, 999999, 1, "queued", "", ""); err == nil {
		t.Fatal("expected error updating a non-existent delivery id")
	}
}

func TestSystemDB_LatestGitHubDispatchDelivery(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_latest", Name: "Latest", FolderPath: "./data/projects/proj_latest"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	if got, err := db.LatestGitHubDispatchDelivery(ctx, proj.ID); err != nil || got != nil {
		t.Fatalf("expected (nil, nil) with no deliveries yet: %v, %+v", err, got)
	}

	generic := &Webhook{ID: "wh-generic", ProjectID: proj.ID, Kind: "generic", URL: "https://x", Secret: "s", Events: []string{"content.publish"}}
	if err := db.CreateWebhook(ctx, generic); err != nil {
		t.Fatal(err)
	}
	dispatch := &Webhook{ID: "wh-dispatch", ProjectID: proj.ID, Kind: "github_dispatch", Config: `{"owner":"o","repo":"r","token":"t"}`, Events: []string{"content.publish"}}
	if err := db.CreateWebhook(ctx, dispatch); err != nil {
		t.Fatal(err)
	}

	// A generic delivery must never surface here, even though it's more
	// "recent" by insertion order than nothing at all.
	if err := db.RecordWebhookDelivery(ctx, &WebhookDelivery{WebhookID: generic.ID, ProjectID: proj.ID, Event: "content.publish", Success: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := db.LatestGitHubDispatchDelivery(ctx, proj.ID); err != nil || got != nil {
		t.Fatalf("a generic delivery must not match: %v, %+v", err, got)
	}

	// A failed dispatch call (GitHub rejected it) shouldn't surface either
	// -- nothing downstream was ever triggered to track.
	if err := db.RecordWebhookDelivery(ctx, &WebhookDelivery{WebhookID: dispatch.ID, ProjectID: proj.ID, Event: "content.publish", Success: false, Error: "unexpected status code 401"}); err != nil {
		t.Fatal(err)
	}
	if got, err := db.LatestGitHubDispatchDelivery(ctx, proj.ID); err != nil || got != nil {
		t.Fatalf("a failed dispatch must not match: %v, %+v", err, got)
	}

	if err := db.RecordWebhookDelivery(ctx, &WebhookDelivery{WebhookID: dispatch.ID, ProjectID: proj.ID, Event: "content.publish", Success: true, StatusCode: 204}); err != nil {
		t.Fatal(err)
	}
	got, err := db.LatestGitHubDispatchDelivery(ctx, proj.ID)
	if err != nil || got == nil {
		t.Fatalf("expected the successful dispatch delivery: %v, %+v", err, got)
	}
	if got.WebhookID != dispatch.ID {
		t.Fatalf("expected the github_dispatch delivery, got webhook %q", got.WebhookID)
	}
}

func TestSystemDB_ListPendingGitHubDispatchDeliveries(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_pending", Name: "Pending", FolderPath: "./data/projects/proj_pending"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}
	dispatch := &Webhook{ID: "wh-pending", ProjectID: proj.ID, Kind: "github_dispatch", Config: `{"owner":"o","repo":"r","token":"t"}`, Events: []string{"content.publish"}}
	if err := db.CreateWebhook(ctx, dispatch); err != nil {
		t.Fatal(err)
	}
	generic := &Webhook{ID: "wh-pending-generic", ProjectID: proj.ID, Kind: "generic", URL: "https://x", Secret: "s", Events: []string{"content.publish"}}
	if err := db.CreateWebhook(ctx, generic); err != nil {
		t.Fatal(err)
	}

	if err := db.RecordWebhookDelivery(ctx, &WebhookDelivery{WebhookID: dispatch.ID, ProjectID: proj.ID, Event: "content.publish", Success: true}); err != nil {
		t.Fatal(err) // pending: never correlated yet (deploy_status == "")
	}
	if err := db.RecordWebhookDelivery(ctx, &WebhookDelivery{WebhookID: generic.ID, ProjectID: proj.ID, Event: "content.publish", Success: true}); err != nil {
		t.Fatal(err) // must never appear: not a github_dispatch webhook
	}
	if err := db.RecordWebhookDelivery(ctx, &WebhookDelivery{WebhookID: dispatch.ID, ProjectID: proj.ID, Event: "content.publish", Success: false}); err != nil {
		t.Fatal(err) // must never appear: the dispatch call itself failed
	}

	pending, err := db.ListPendingGitHubDispatchDeliveries(ctx, time.Hour)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].WebhookID != dispatch.ID {
		t.Fatalf("expected exactly the one uncorrelated github_dispatch delivery, got %+v", pending)
	}

	// Once resolved, it must drop out of the pending set.
	if err := db.UpdateWebhookDeliveryDeployState(ctx, pending[0].ID, 1, "completed", "success", "https://x/1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	pending, err = db.ListPendingGitHubDispatchDeliveries(ctx, time.Hour)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected an empty pending set once resolved: %v (%d)", err, len(pending))
	}
}

func TestSystemDB_SetLastProject(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	u := &User{ID: uuid.NewString(), Email: "lastproj@example.com", PasswordHash: "hash"}
	if err := db.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetUserByID(ctx, u.ID)
	if got.LastProjectID != nil {
		t.Fatalf("expected no last project initially, got %+v", got.LastProjectID)
	}

	if err := db.SetLastProject(ctx, u.ID, "proj_abc"); err != nil {
		t.Fatalf("set last project: %v", err)
	}
	got2, _ := db.GetUserByID(ctx, u.ID)
	if got2.LastProjectID == nil || *got2.LastProjectID != "proj_abc" {
		t.Fatalf("expected last project proj_abc, got %+v", got2.LastProjectID)
	}
}

func TestSystemDB_Webhook_GithubDispatchKind(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_ghd", Name: "GHD", FolderPath: "./data/projects/proj_ghd"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	cfg := `{"owner":"louis","repo":"site","token":"encrypted-blob"}`
	wh := &Webhook{
		ID:        uuid.NewString(),
		ProjectID: proj.ID,
		Kind:      "github_dispatch",
		Config:    cfg,
		Events:    []string{"content.publish", "content.unpublish"},
	}
	if err := db.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	got, err := db.GetWebhook(ctx, wh.ID)
	if err != nil {
		t.Fatalf("get webhook: %v", err)
	}
	if got.Kind != "github_dispatch" || got.Config != cfg {
		t.Fatalf("expected kind/config to round-trip, got kind=%q config=%q", got.Kind, got.Config)
	}

	list, err := db.ListWebhooks(ctx, proj.ID)
	if err != nil || len(list) != 1 || list[0].Config != cfg {
		t.Fatalf("expected 1 webhook with config preserved via List: %v %+v", err, list)
	}

	wh.Config = `{"owner":"louis","repo":"site2","token":"new-encrypted-blob"}`
	if err := db.UpdateWebhook(ctx, wh); err != nil {
		t.Fatalf("update webhook: %v", err)
	}
	got2, _ := db.GetWebhook(ctx, wh.ID)
	if got2.Config != wh.Config {
		t.Fatalf("expected updated config to round-trip, got %q", got2.Config)
	}
}

func TestSystemDB_Webhook_DefaultKindIsGeneric(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	proj := &Project{ID: "proj_default_kind", Name: "P", FolderPath: "./data/projects/proj_default_kind"}
	if err := db.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	wh := &Webhook{ID: uuid.NewString(), ProjectID: proj.ID, URL: "https://example.com", Secret: "s", Events: []string{"content.create"}}
	if err := db.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	got, err := db.GetWebhook(ctx, wh.ID)
	if err != nil || got.Kind != "generic" {
		t.Fatalf("expected default kind 'generic', got %q (err=%v)", got.Kind, err)
	}
}

// TestMigrateWebhookColumns_IdempotentOnPreExistingTable simulates a
// system.db created before the kind/config columns existed: a webhooks
// table with only the original columns. OpenSystemDB must add the new
// columns on top without failing, and running it twice must not error
// either (ALTER TABLE ADD COLUMN is not naturally idempotent in SQLite).
func TestMigrateWebhookColumns_IdempotentOnPreExistingTable(t *testing.T) {
	db, err := OpenSystemDB(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Drop down to the pre-migration shape and re-run the migration twice.
	if _, err := db.db.Exec(`DROP TABLE webhooks`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := db.db.Exec(`CREATE TABLE webhooks (
		id TEXT PRIMARY KEY,
		project_id TEXT,
		url TEXT NOT NULL,
		secret TEXT NOT NULL,
		events TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("recreate legacy table: %v", err)
	}

	if err := migrateWebhookColumns(db.db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := migrateWebhookColumns(db.db); err != nil {
		t.Fatalf("second (idempotent) migration: %v", err)
	}

	// Columns should now be usable via the normal CRUD path.
	ctx := context.Background()
	wh := &Webhook{ID: uuid.NewString(), ProjectID: "p", Kind: "github_dispatch", Config: `{"owner":"a","repo":"b"}`, Events: []string{"content.publish"}}
	if err := db.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("create webhook after migration: %v", err)
	}
	got, err := db.GetWebhook(ctx, wh.ID)
	if err != nil || got.Kind != "github_dispatch" {
		t.Fatalf("expected migrated columns to work, got %+v (err=%v)", got, err)
	}
}

func TestSystemDB_GlobalLogs(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)

	if err := db.LogAction(ctx, "user-1", "proj-1", "project.create", map[string]string{"name": "Demo"}); err != nil {
		t.Fatalf("log action: %v", err)
	}
	if err := db.LogAction(ctx, "", "", "user.suspend", nil); err != nil {
		t.Fatalf("log action without ids: %v", err)
	}

	logs, err := db.ListLogs(ctx, 10)
	if err != nil || len(logs) != 2 {
		t.Fatalf("expected 2 logs: %v (%d)", err, len(logs))
	}
	// Most recent first.
	if logs[0].Action != "user.suspend" {
		t.Fatalf("expected most recent log first, got %s", logs[0].Action)
	}
}

// TestSystemDB_ProjectLastActivity guards against a regression where
// MAX(created_at) -- a computed aggregate column with no declared SQL type
// of its own, unlike a plain "SELECT created_at" -- came back from the
// driver as a raw string instead of an auto-converted time.Time, and
// scanning that into a sql.NullTime failed outright. The failure was
// silent in production (htmxServerError logs nothing), so this needs to be
// caught here, not just noticed by a human staring at a blank error page.
func TestSystemDB_ProjectLastActivity(t *testing.T) {
	ctx := context.Background()
	db := newTestSystemDB(t)
	createdAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// No activity yet: falls back to the project's own creation time.
	last, err := db.ProjectLastActivity(ctx, "proj-1", createdAt)
	if err != nil {
		t.Fatalf("ProjectLastActivity with no logs: %v", err)
	}
	if !last.Equal(createdAt) {
		t.Fatalf("expected fallback to createdAt %v, got %v", createdAt, last)
	}

	// A log entry logged normally (CURRENT_TIMESTAMP, "now") must not error
	// out -- this is the exact regression: the aggregate scan used to fail.
	if err := db.LogAction(ctx, "user-1", "proj-1", "content.create", nil); err != nil {
		t.Fatalf("log action: %v", err)
	}
	if _, err := db.ProjectLastActivity(ctx, "proj-1", createdAt); err != nil {
		t.Fatalf("ProjectLastActivity after a real LogAction: %v", err)
	}

	// A log entry with a controlled, later timestamp is correctly picked up
	// over the fallback.
	later := createdAt.Add(48 * time.Hour)
	if _, err := db.DB().ExecContext(ctx,
		`INSERT INTO global_logs (project_id, action, created_at) VALUES (?, ?, ?)`,
		"proj-2", "content.update", later.Format("2006-01-02 15:04:05"),
	); err != nil {
		t.Fatalf("insert log with explicit timestamp: %v", err)
	}
	last, err = db.ProjectLastActivity(ctx, "proj-2", createdAt)
	if err != nil {
		t.Fatalf("ProjectLastActivity with explicit later log: %v", err)
	}
	if !last.Equal(later) {
		t.Fatalf("expected last activity %v, got %v", later, last)
	}
}

func TestOpenSystemDB_InvalidPath(t *testing.T) {
	// A path inside a directory that doesn't exist cannot be created by SQLite.
	if _, err := OpenSystemDB("/nonexistent-dir-xyz/system.db"); err == nil {
		t.Fatal("expected error opening system db at an unwritable path")
	}
}

func TestSystemDB_DBAccessor(t *testing.T) {
	db := newTestSystemDB(t)
	if db.DB() == nil {
		t.Fatal("expected non-nil *sql.DB")
	}
}
