package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// tokenView omits ProjectID (storage.APIToken.ProjectID is now vestigial):
// every token minted through this handler is global and ADMIN-equivalent,
// so there is nothing project-scoped left worth exposing.
type tokenView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toTokenView(t *storage.APIToken) tokenView {
	return tokenView{ID: t.ID, Name: t.Name}
}

// handleListAPITokens lists every API token (never their plaintext or
// hash). Global scope, managed from Administration -- global-admin only
// (spec §1: "gestion des tokens API").
func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.System.ListAPITokens(r.Context())
	if err != nil {
		writeStorageError(w, err)
		return
	}
	out := make([]tokenView, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toTokenView(t))
	}
	writeJSON(w, http.StatusOK, out)
}

type createTokenRequest struct {
	Name string `json:"name"`
}

type createTokenResponse struct {
	tokenView
	Token string `json:"token"` // shown once, per spec §2.1
}

// handleCreateAPIToken mints a new API token. It is always global and
// ADMIN-equivalent (see pkg/auth middleware): any bearer of this token can
// do everything a global ADMIN can through the API, with the single
// exception of deleting a project (no API route exists for that -- see
// router.go). The plaintext is returned exactly once; only its hash is
// persisted. Global-admin only to create.
func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "field 'name' is required")
		return
	}

	plaintext, err := auth.GenerateAPIToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate token")
		return
	}
	t := &storage.APIToken{
		ID:        uuid.NewString(),
		TokenHash: auth.HashAPIToken(plaintext),
		Name:      req.Name,
	}
	if err := s.System.CreateAPIToken(r.Context(), t); err != nil {
		writeStorageError(w, err)
		return
	}
	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), "", "token.create", map[string]string{"name": t.Name})
	writeJSON(w, http.StatusCreated, createTokenResponse{tokenView: toTokenView(t), Token: plaintext})
}

// handleDeleteAPIToken revokes an API token. Global-admin only.
func (s *Server) handleDeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	tokenID := chi.URLParam(r, "tokenID")
	if err := s.System.DeleteAPIToken(r.Context(), tokenID); err != nil {
		writeStorageError(w, err)
		return
	}
	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), "", "token.delete", map[string]string{"id": tokenID})
	w.WriteHeader(http.StatusNoContent)
}
