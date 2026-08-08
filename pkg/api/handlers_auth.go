package api

import (
	"errors"
	"net/http"
	"time"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userView struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	IsGlobalAdmin bool   `json:"is_global_admin"`
}

func toUserView(u *storage.User) userView {
	return userView{ID: u.ID, Email: u.Email, IsGlobalAdmin: u.IsGlobalAdmin}
}

// handleLogin authenticates an email/password pair and, on success, sets a
// signed session cookie carrying a JWT (pkg/auth).
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.System.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeStorageError(w, err)
		return
	}

	if err := auth.VerifyPassword(user.PasswordHash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := s.Issuer.Issue(user.ID, user.IsGlobalAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue session")
		return
	}
	s.clearBootstrapFlag()

	http.SetCookie(w, &http.Cookie{
		Name:     s.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	_ = s.System.LogAction(r.Context(), user.ID, "", "user.login", nil)
	writeJSON(w, http.StatusOK, toUserView(user))
}

// handleLogout clears the session cookie. It's intentionally permissive
// about missing/invalid sessions -- logging out is always a no-op success.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// meTokenView is what GET /me returns when authenticated via API token
// rather than a session: there's no storage.User record to describe, but
// callers (scripts, the tricms-setup utility) can still use this to sanity
// check that their token is valid and confirm it's admin-equivalent.
type meTokenView struct {
	AuthType      string `json:"auth_type"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	IsGlobalAdmin bool   `json:"is_global_admin"`
}

// handleMe returns the currently authenticated identity, useful for the
// front-end (or an external script) to bootstrap its session/token state.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if user := UserFromContext(r.Context()); user != nil {
		writeJSON(w, http.StatusOK, toUserView(user))
		return
	}
	if tok := APITokenFromContext(r.Context()); tok != nil {
		writeJSON(w, http.StatusOK, meTokenView{AuthType: "token", ID: tok.ID, Name: tok.Name, IsGlobalAdmin: true})
		return
	}
	writeError(w, http.StatusUnauthorized, "authentication required")
}
