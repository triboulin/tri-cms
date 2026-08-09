package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

// handleListWebhooks lists a project's webhooks. Global-admin only
// (spec §4.2 sidebar: "[Webhooks] visible uniquement si rôle == ADMIN").
func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	whs, err := s.System.ListWebhooks(r.Context(), project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, whs)
}

// webhookRequest covers both webhook kinds. For kind="generic" (the
// default when Kind is omitted, preserving the original API contract):
// URL and Secret are required. For kind="github_dispatch": GitHubOwner and
// GitHubRepo are always required, GitHubToken is required on create and
// optional on update (blank = keep the previously stored token).
type webhookRequest struct {
	Kind   string   `json:"kind"`
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events"`

	GitHubOwner string `json:"github_owner"`
	GitHubRepo  string `json:"github_repo"`
	GitHubToken string `json:"github_token"`
}

// buildWebhookFields validates req and returns the (kind, url, secret,
// config) tuple to persist. existing is the webhook being updated (nil on
// create) -- used to fall back to the previously stored, still-encrypted
// GitHub token when req.GitHubToken is left blank on an update, so editing
// a github_dispatch webhook's events doesn't force re-entering the PAT.
// req is taken by pointer so that an omitted/empty Events list can be
// defaulted to every known event type -- a webhook subscribes to
// everything unless the caller deliberately narrows it down.
func (s *Server) buildWebhookFields(req *webhookRequest, existing *storage.Webhook) (kind, url, secret, config, problem string) {
	if len(req.Events) == 0 {
		req.Events = availableWebhookEvents()
	}
	kind = req.Kind
	if kind == "" {
		kind = webhooks.KindGeneric
	}

	switch kind {
	case webhooks.KindGeneric:
		if req.URL == "" || req.Secret == "" {
			return "", "", "", "", "fields 'url' and 'secret' are required for kind=generic"
		}
		return kind, req.URL, req.Secret, "", ""

	case webhooks.KindGitHubDispatch:
		if req.GitHubOwner == "" || req.GitHubRepo == "" {
			return "", "", "", "", "fields 'github_owner' and 'github_repo' are required for kind=github_dispatch"
		}
		encryptedToken := ""
		if req.GitHubToken != "" {
			if s.Encryptor == nil {
				return "", "", "", "", "server is not configured with an encryption key (TRICMS_ENCRYPTION_KEY); cannot store a GitHub token"
			}
			enc, err := s.Encryptor.Encrypt(req.GitHubToken)
			if err != nil {
				return "", "", "", "", "failed to encrypt github_token"
			}
			encryptedToken = enc
		} else if existing != nil && existing.Kind == webhooks.KindGitHubDispatch {
			var prev webhooks.GitHubDispatchConfig
			_ = json.Unmarshal([]byte(existing.Config), &prev)
			encryptedToken = prev.Token
		}
		if encryptedToken == "" {
			return "", "", "", "", "field 'github_token' is required (no existing token to keep)"
		}
		cfg, err := json.Marshal(webhooks.GitHubDispatchConfig{Owner: req.GitHubOwner, Repo: req.GitHubRepo, Token: encryptedToken})
		if err != nil {
			return "", "", "", "", "failed to encode github_dispatch config"
		}
		return kind, "", "", string(cfg), ""

	default:
		return "", "", "", "", "unknown 'kind': must be 'generic' or 'github_dispatch'"
	}
}

// handleCreateWebhook registers a new webhook subscription. Global-admin only.
func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	var req webhookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kind, url, secret, config, problem := s.buildWebhookFields(&req, nil)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	wh := &storage.Webhook{ID: uuid.NewString(), ProjectID: project.ID, Kind: kind, URL: url, Secret: secret, Config: config, Events: req.Events}
	if err := s.System.CreateWebhook(r.Context(), wh); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

// handleUpdateWebhook updates a webhook's URL/secret/events (or, for
// kind=github_dispatch, its owner/repo/token). Global-admin only.
func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	webhookID := chi.URLParam(r, "webhookID")
	var req webhookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing, err := s.System.GetWebhook(r.Context(), webhookID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	kind, url, secret, config, problem := s.buildWebhookFields(&req, existing)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	wh := &storage.Webhook{ID: webhookID, Kind: kind, URL: url, Secret: secret, Config: config, Events: req.Events}
	if err := s.System.UpdateWebhook(r.Context(), wh); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wh)
}

// handleDeleteWebhook removes a webhook subscription. Global-admin only.
func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	webhookID := chi.URLParam(r, "webhookID")
	if err := s.System.DeleteWebhook(r.Context(), webhookID); err != nil {
		writeStorageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
