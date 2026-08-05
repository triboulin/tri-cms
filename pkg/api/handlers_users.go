package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// handleListUsers lists every global user account. Global-admin only
// (spec §4.3 "Gestion des Comptes Globaux").
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.System.ListUsers(r.Context())
	if err != nil {
		writeStorageError(w, err)
		return
	}
	out := make([]userView, 0, len(users))
	for _, u := range users {
		out = append(out, toUserView(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type createUserRequest struct {
	Email         string `json:"email"`
	Password      string `json:"password"`
	IsGlobalAdmin bool   `json:"is_global_admin"`
}

// handleCreateUser creates a new global user account. Global-admin only.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "fields 'email' and 'password' are required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := &storage.User{ID: uuid.NewString(), Email: req.Email, PasswordHash: hash, IsGlobalAdmin: req.IsGlobalAdmin}
	if err := s.System.CreateUser(r.Context(), u); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toUserView(u))
}

type updateUserRequest struct {
	Password      *string `json:"password,omitempty"`
	IsGlobalAdmin *bool   `json:"is_global_admin,omitempty"`
}

// handleUpdateUser changes a user's password and/or global-admin flag.
// Global-admin only.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Password != nil {
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.System.UpdateUserPassword(r.Context(), userID, hash); err != nil {
			writeStorageError(w, err)
			return
		}
	}
	if req.IsGlobalAdmin != nil {
		if err := s.System.SetGlobalAdmin(r.Context(), userID, *req.IsGlobalAdmin); err != nil {
			writeStorageError(w, err)
			return
		}
	}
	u, err := s.System.GetUserByID(r.Context(), userID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserView(u))
}

// handleDeleteUser removes (suspends, permanently) a global user account.
// Global-admin only.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	if err := s.System.DeleteUser(r.Context(), userID); err != nil {
		writeStorageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
