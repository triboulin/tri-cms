package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

type contentResponse struct {
	ID         string                `json:"id"`
	SchemaSlug string                `json:"schema_slug"`
	Data       map[string]any        `json:"data"`
	Status     storage.ContentStatus `json:"status"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

func toContentResponse(c *storage.Content) (*contentResponse, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(c.Data), &data); err != nil {
		return nil, err
	}
	return &contentResponse{ID: c.ID, SchemaSlug: c.SchemaSlug, Data: data, Status: c.Status, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}, nil
}

// schemaHooks builds pkg/schema validation hooks backed by this project's
// storage: Media/Reference existence checks and Slug uniqueness, scanning
// existing content the same way pkg/storage.CountContentsReferencing does.
func (s *Server) schemaHooks(ctx context.Context, db *storage.ProjectDB, def *trischema.Definition, excludeContentID string) *trischema.Hooks {
	return &trischema.Hooks{
		MediaExists: func(mediaID string) (bool, error) {
			_, err := db.GetMedia(ctx, mediaID)
			if err != nil {
				if err == storage.ErrNotFound {
					return false, nil
				}
				return false, err
			}
			return true, nil
		},
		ReferenceExists: func(targetSchema, contentID string) (bool, error) {
			c, err := db.GetContent(ctx, contentID)
			if err != nil {
				if err == storage.ErrNotFound {
					return false, nil
				}
				return false, err
			}
			return c.SchemaSlug == targetSchema, nil
		},
		SlugTaken: func(schemaSlug, value, exclude string) (bool, error) {
			existing, err := db.ListContents(ctx, schemaSlug)
			if err != nil {
				return false, err
			}
			for _, c := range existing {
				if c.ID == exclude {
					continue
				}
				var data map[string]any
				if err := json.Unmarshal([]byte(c.Data), &data); err != nil {
					continue
				}
				for _, f := range def.Fields {
					if f.Type != trischema.Slug {
						continue
					}
					if s, ok := data[f.Key].(string); ok && s == value {
						return true, nil
					}
				}
			}
			return false, nil
		},
	}
}

func (s *Server) loadSchemaDefinition(ctx context.Context, db *storage.ProjectDB, slug string) (*storage.Schema, *trischema.Definition, error) {
	sc, err := db.GetSchema(ctx, slug)
	if err != nil {
		return nil, nil, err
	}
	def, err := trischema.ParseDefinition([]byte(sc.Definition))
	if err != nil {
		return nil, nil, err
	}
	return sc, def, nil
}

// handleListContents lists every content instance of one schema.
func (s *Server) handleListContents(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	schemaSlug := chi.URLParam(r, "schemaSlug")
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	items, err := db.ListContents(r.Context(), schemaSlug)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	out := make([]*contentResponse, 0, len(items))
	for _, c := range items {
		cr, err := toContentResponse(c)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "corrupt content record")
			return
		}
		out = append(out, cr)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetContent fetches one content instance by id.
func (s *Server) handleGetContent(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	contentID := chi.URLParam(r, "contentID")
	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	c, err := db.GetContent(r.Context(), contentID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	cr, err := toContentResponse(c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "corrupt content record")
		return
	}
	writeJSON(w, http.StatusOK, cr)
}

type contentRequest struct {
	Data   map[string]any        `json:"data"`
	Status storage.ContentStatus `json:"status,omitempty"`
}

// handleCreateContent validates `req.Data` against the schema's definition
// (pkg/schema), stamps root-level created_at/updated_at (spec §3 note),
// persists it, and fires any subscribed content.create webhooks.
// REDACTEUR+ only.
func (s *Server) handleCreateContent(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	schemaSlug := chi.URLParam(r, "schemaSlug")

	var req contentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status == "" {
		req.Status = storage.StatusDraft
	}
	if req.Status != storage.StatusDraft && req.Status != storage.StatusPublished {
		writeError(w, http.StatusBadRequest, "status must be 'draft' or 'published'")
		return
	}

	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	_, def, err := s.loadSchemaDefinition(r.Context(), db, schemaSlug)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	normalized, err := trischema.ValidateAndNormalize(def, schemaSlug, req.Data, "", s.schemaHooks(r.Context(), db, def, ""))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	normalized["created_at"] = now
	normalized["updated_at"] = now

	dataJSON, err := json.Marshal(normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode content")
		return
	}

	c := &storage.Content{ID: uuid.NewString(), SchemaSlug: schemaSlug, Data: string(dataJSON), Status: req.Status}
	if err := db.CreateContent(r.Context(), c); err != nil {
		writeStorageError(w, err)
		return
	}

	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), project.ID, "content.create", map[string]string{"schema": schemaSlug, "id": c.ID})
	s.dispatchWebhooksAsync(r.Context(), project.ID, webhooks.EventContentUpdate, map[string]string{"id": c.ID, "schema": schemaSlug})

	cr, _ := toContentResponse(c)
	writeJSON(w, http.StatusCreated, cr)
}

// handleUpdateContent re-validates and replaces a content's data and/or
// status. REDACTEUR+ only.
func (s *Server) handleUpdateContent(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	schemaSlug := chi.URLParam(r, "schemaSlug")
	contentID := chi.URLParam(r, "contentID")

	var req contentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	existing, err := db.GetContent(r.Context(), contentID)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	_, def, err := s.loadSchemaDefinition(r.Context(), db, schemaSlug)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	normalized, err := trischema.ValidateAndNormalize(def, schemaSlug, req.Data, contentID, s.schemaHooks(r.Context(), db, def, contentID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var previous map[string]any
	_ = json.Unmarshal([]byte(existing.Data), &previous)
	if createdAt, ok := previous["created_at"]; ok {
		normalized["created_at"] = createdAt
	}
	normalized["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	dataJSON, err := json.Marshal(normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode content")
		return
	}

	status := existing.Status
	if req.Status != "" {
		if req.Status != storage.StatusDraft && req.Status != storage.StatusPublished {
			writeError(w, http.StatusBadRequest, "status must be 'draft' or 'published'")
			return
		}
		status = req.Status
	}

	existing.Data = string(dataJSON)
	existing.Status = status
	if err := db.UpdateContent(r.Context(), existing); err != nil {
		writeStorageError(w, err)
		return
	}

	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), project.ID, "content.update", map[string]string{"schema": schemaSlug, "id": contentID})
	s.dispatchWebhooksAsync(r.Context(), project.ID, webhooks.EventContentUpdate, map[string]string{"id": contentID, "schema": schemaSlug})

	cr, _ := toContentResponse(existing)
	writeJSON(w, http.StatusOK, cr)
}

// handleDeleteContent deletes a content instance. If other content still
// references it via a Reference field, deletion is refused unless the
// caller passes ?force=true, matching spec §3's requirement for explicit
// confirmation before breaking references.
func (s *Server) handleDeleteContent(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	schemaSlug := chi.URLParam(r, "schemaSlug")
	contentID := chi.URLParam(r, "contentID")
	force := strings.EqualFold(r.URL.Query().Get("force"), "true")

	db, err := s.projectDB(project.ID)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	if !force {
		n, err := db.CountContentsReferencing(r.Context(), contentID)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		if n > 0 {
			writeError(w, http.StatusConflict, "content is still referenced by other content; pass ?force=true to delete anyway")
			return
		}
	}

	if err := db.DeleteContent(r.Context(), contentID); err != nil {
		writeStorageError(w, err)
		return
	}

	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), project.ID, "content.delete", map[string]string{"schema": schemaSlug, "id": contentID})
	s.dispatchWebhooksAsync(r.Context(), project.ID, webhooks.EventContentUpdate, map[string]string{"id": contentID, "schema": schemaSlug})
	w.WriteHeader(http.StatusNoContent)
}

// dispatchWebhooksAsync fires every webhook subscribed to `event` for a
// project without blocking the HTTP response: only the (cheap, indexed)
// lookup of subscribers runs synchronously, so the return value -- whether
// any webhook was actually subscribed -- is available immediately, letting
// callers tell the user "redéploiement en cours" only when a delivery is
// genuinely about to happen rather than unconditionally. Every attempt is
// recorded via RecordWebhookDelivery (success or failure) so the Webhooks
// page can show a real history; delivery retry/backoff is handled inside
// pkg/webhooks.Dispatcher.
func (s *Server) dispatchWebhooksAsync(ctx context.Context, projectID, event string, payload any) bool {
	if s.Dispatcher == nil {
		return false
	}
	whs, err := s.System.ListWebhooksForEvent(ctx, projectID, event)
	if err != nil || len(whs) == 0 {
		return false
	}
	go func() {
		bgCtx := context.Background()
		for _, wh := range whs {
			res, err := s.Dispatcher.Send(bgCtx, wh, event, payload)
			d := &storage.WebhookDelivery{WebhookID: wh.ID, ProjectID: projectID, Event: event}
			switch {
			case err != nil:
				d.Error = err.Error()
			case res != nil:
				d.Success, d.Attempts, d.StatusCode, d.Error = res.Success, res.Attempts, res.LastStatusCode, res.LastError
			}
			_ = s.System.RecordWebhookDelivery(bgCtx, d)
			if !d.Success {
				_ = s.System.LogAction(bgCtx, "", projectID, "webhook.delivery_failed", map[string]any{"webhook_id": wh.ID, "event": event})
			}
		}
	}()
	return true
}
