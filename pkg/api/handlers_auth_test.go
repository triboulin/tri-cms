package api

import (
	"net/http"
	"testing"
)

func TestLogin_Success(t *testing.T) {
	e := newTestEnv(t)
	u := e.createUser("alice@example.com", false)

	rec := e.request(http.MethodPost, "/api/v1/auth/login", nil, loginRequest{Email: "alice@example.com", Password: "password123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[userView](t, rec)
	if got.ID != u.ID || got.Email != "alice@example.com" {
		t.Fatalf("unexpected user view: %+v", got)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected a session cookie to be set")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	e := newTestEnv(t)
	e.createUser("bob@example.com", false)
	rec := e.request(http.MethodPost, "/api/v1/auth/login", nil, loginRequest{Email: "bob@example.com", Password: "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	e := newTestEnv(t)
	rec := e.request(http.MethodPost, "/api/v1/auth/login", nil, loginRequest{Email: "nobody@example.com", Password: "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLogin_MalformedBody(t *testing.T) {
	e := newTestEnv(t)
	req := e.request(http.MethodPost, "/api/v1/auth/login", nil, map[string]int{"email": 1})
	if req.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", req.Code)
	}
}

func TestMe_RequiresAuth(t *testing.T) {
	e := newTestEnv(t)
	rec := e.request(http.MethodGet, "/api/v1/me", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	u := e.createUser("carol@example.com", true)
	rec = e.request(http.MethodGet, "/api/v1/me", u, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got := decodeBody[userView](t, rec)
	if !got.IsGlobalAdmin {
		t.Fatal("expected is_global_admin=true")
	}
}

func TestLogout_ClearsCookie(t *testing.T) {
	e := newTestEnv(t)
	u := e.createUser("dave@example.com", false)
	rec := e.request(http.MethodPost, "/api/v1/auth/logout", u, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected cookie to be expired, got %+v", cookies)
	}
}
