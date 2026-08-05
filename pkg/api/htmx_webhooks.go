package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

// availableWebhookEvents lists every event a webhook can subscribe to, for
// the create/edit form's checkbox list.
func availableWebhookEvents() []string {
	return []string{
		webhooks.EventContentCreate, webhooks.EventContentUpdate, webhooks.EventContentDelete,
		webhooks.EventContentPublish, webhooks.EventContentUnpublish,
		webhooks.EventMediaCreate, webhooks.EventMediaDelete,
	}
}

type webhookEventOption struct {
	Value    string
	Selected bool
}

func webhookEventOptions(selected []string) []webhookEventOption {
	set := map[string]bool{}
	for _, e := range selected {
		set[e] = true
	}
	opts := make([]webhookEventOption, 0)
	for _, e := range availableWebhookEvents() {
		opts = append(opts, webhookEventOption{Value: e, Selected: set[e]})
	}
	return opts
}

type webhookRowVM struct {
	Webhook *storage.Webhook
	Events  []webhookEventOption
}

// htmxWebhooks renders the webhook management page (spec §4.2 "[Webhooks]",
// global-admin only). This page previously didn't exist even though the
// sidebar linked to it.
func (s *Server) htmxWebhooks(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	whs, err := s.System.ListWebhooks(r.Context(), project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	rows := make([]webhookRowVM, 0, len(whs))
	for _, wh := range whs {
		rows = append(rows, webhookRowVM{Webhook: wh, Events: webhookEventOptions(wh.Events)})
	}
	content := struct {
		Webhooks        []webhookRowVM
		NewEventOptions []webhookEventOption
	}{rows, webhookEventOptions(nil)}

	data, err := s.buildPageData(r.Context(), user, project, "webhooks", "", content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:webhooks", data)
}

func (s *Server) htmxCreateWebhook(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/webhooks"
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	url := r.FormValue("url")
	secret := r.FormValue("secret")
	events := r.Form["events"]
	if url == "" || secret == "" || len(events) == 0 {
		redirectWithFlash(w, r, back, "URL, secret et au moins un évènement sont requis.", "error")
		return
	}
	wh := &storage.Webhook{ID: uuid.NewString(), ProjectID: project.ID, URL: url, Secret: secret, Events: events}
	if err := s.System.CreateWebhook(r.Context(), wh); err != nil {
		redirectWithFlash(w, r, back, "Création impossible : "+err.Error(), "error")
		return
	}
	redirectWithFlash(w, r, back, "Webhook créé.", "success")
}

func (s *Server) htmxUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/webhooks"
	webhookID := chi.URLParam(r, "webhookID")
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	url := r.FormValue("url")
	secret := r.FormValue("secret")
	events := r.Form["events"]
	if url == "" || secret == "" || len(events) == 0 {
		redirectWithFlash(w, r, back, "URL, secret et au moins un évènement sont requis.", "error")
		return
	}
	wh := &storage.Webhook{ID: webhookID, URL: url, Secret: secret, Events: events}
	if err := s.System.UpdateWebhook(r.Context(), wh); err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	redirectWithFlash(w, r, back, "Webhook mis à jour.", "success")
}

func (s *Server) htmxDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionWebhooks)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/webhooks"
	webhookID := chi.URLParam(r, "webhookID")
	if err := s.System.DeleteWebhook(r.Context(), webhookID); err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	redirectWithFlash(w, r, back, "Webhook supprimé.", "success")
}
