package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/storage"
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

type webhookRequest struct {
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events"`
}

// handleCreateWebhook registers a new webhook subscription. Global-admin only.
func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	var req webhookRequest
	if err := decodeJSON(r, &req); err != nil || req.URL == "" || req.Secret == "" || len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "fields 'url', 'secret' and non-empty 'events' are required")
		return
	}
	wh := &storage.Webhook{ID: uuid.NewString(), ProjectID: project.ID, URL: req.URL, Secret: req.Secret, Events: req.Events}
	if err := s.System.CreateWebhook(r.Context(), wh); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

// handleUpdateWebhook updates a webhook's URL/secret/events. Global-admin only.
func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	webhookID := chi.URLParam(r, "webhookID")
	var req webhookRequest
	if err := decodeJSON(r, &req); err != nil || req.URL == "" || req.Secret == "" || len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "fields 'url', 'secret' and non-empty 'events' are required")
		return
	}
	wh := &storage.Webhook{ID: webhookID, URL: req.URL, Secret: req.Secret, Events: req.Events}
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
