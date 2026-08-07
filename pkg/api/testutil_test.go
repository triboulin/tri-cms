package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

// testEnv wires a full Server against temp-dir-backed storage, without
// templates (JSON API only), for fast end-to-end httptest coverage.
type testEnv struct {
	t      *testing.T
	server *Server
	router http.Handler
	issuer *auth.TokenIssuer
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	auth.DefaultBcryptCost = 4 // keep test hashing fast

	sys, err := storage.OpenSystemDB(":memory:")
	if err != nil {
		t.Fatalf("open system db: %v", err)
	}
	t.Cleanup(func() { sys.Close() })

	base := t.TempDir()
	mgr := storage.NewManager(base)

	issuer, err := auth.NewTokenIssuer([]byte("test-secret-key"), time.Hour)
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	dispatcher := webhooks.NewDispatcher()
	dispatcher.MaxAttempts = 1 // don't slow tests down with retries

	encryptor, err := auth.NewEncryptor(auth.DeriveKey("test-encryption-key"))
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	dispatcher.Decryptor = encryptor

	srv := NewServer(sys, mgr, issuer, dispatcher, nil)
	srv.Encryptor = encryptor
	t.Cleanup(func() { srv.Close() })

	return &testEnv{t: t, server: srv, router: NewRouter(srv), issuer: issuer}
}

// createUser inserts a user directly via storage (bypassing HTTP) and
// returns it plus its plaintext password.
func (e *testEnv) createUser(email string, isAdmin bool) *storage.User {
	e.t.Helper()
	hash, err := auth.HashPassword("password123")
	if err != nil {
		e.t.Fatalf("hash password: %v", err)
	}
	u := &storage.User{ID: newUUID(), Email: email, PasswordHash: hash, IsGlobalAdmin: isAdmin}
	if err := e.server.System.CreateUser(bgCtx(), u); err != nil {
		e.t.Fatalf("create user: %v", err)
	}
	return u
}

func (e *testEnv) createProject(name string) *storage.Project {
	e.t.Helper()
	id := "proj_" + newUUID()
	db, err := e.server.Manager.CreateProjectStorage(id)
	if err != nil {
		e.t.Fatalf("create project storage: %v", err)
	}
	db.Close()
	p := &storage.Project{ID: id, Name: name, FolderPath: e.server.Manager.ProjectDir(id)}
	if err := e.server.System.CreateProject(bgCtx(), p); err != nil {
		e.t.Fatalf("create project: %v", err)
	}
	return p
}

func (e *testEnv) setRole(userID, projectID string, role storage.Role) {
	e.t.Helper()
	if err := e.server.System.SetProjectPermission(bgCtx(), &storage.ProjectPermission{UserID: userID, ProjectID: projectID, Role: role}); err != nil {
		e.t.Fatalf("set permission: %v", err)
	}
}

func (e *testEnv) sessionCookie(u *storage.User) *http.Cookie {
	e.t.Helper()
	tok, err := e.issuer.Issue(u.ID, u.IsGlobalAdmin)
	if err != nil {
		e.t.Fatalf("issue token: %v", err)
	}
	return &http.Cookie{Name: e.server.SessionCookieName, Value: tok}
}

// request performs an HTTP request against the router, optionally
// authenticated as user u (nil for anonymous), with a JSON body.
func (e *testEnv) request(method, path string, u *storage.User, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if u != nil {
		req.AddCookie(e.sessionCookie(u))
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) requestWithToken(method, path, token string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// postForm performs an application/x-www-form-urlencoded POST, exactly as
// every HTMX admin-UI <form> does (see pkg/api/htmx*.go).
func (e *testEnv) postForm(path string, u *storage.User, values url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(values.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if u != nil {
		req.AddCookie(e.sessionCookie(u))
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return v
}

func newUUID() string        { return uuid.NewString() }
func bgCtx() context.Context { return context.Background() }
