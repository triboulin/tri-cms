package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

type tokenView struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

func toTokenView(t *storage.APIToken) tokenView {
	return tokenView{ID: t.ID, ProjectID: t.ProjectID, Name: t.Name}
}

// handleListAPITokens lists a project's API tokens (never their plaintext
// or hash). Global-admin only (spec §1: "gestion des tokens API").
func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	tokens, err := s.System.ListAPITokens(r.Context(), project.ID)
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

// handleCreateAPIToken mints a new API token for the in-scope project. The
// plaintext is returned exactly once; only its hash is persisted.
// Global-admin only.
func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
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
		ProjectID: project.ID,
		TokenHash: auth.HashAPIToken(plaintext),
		Name:      req.Name,
	}
	if err := s.System.CreateAPIToken(r.Context(), t); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createTokenResponse{tokenView: toTokenView(t), Token: plaintext})
}

// handleDeleteAPIToken revokes an API token. Global-admin only.
func (s *Server) handleDeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	tokenID := chi.URLParam(r, "tokenID")
	if err := s.System.DeleteAPIToken(r.Context(), tokenID); err != nil {
		writeStorageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
