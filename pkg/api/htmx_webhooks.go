package api

import (
	"encoding/json"
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
	Webhook     *storage.Webhook
	Events      []webhookEventOption
	IsGitHub    bool
	GitHubOwner string
	GitHubRepo  string
}

// githubDispatchDisplayFields extracts the non-secret parts (owner/repo) of
// a github_dispatch webhook's Config, for prefilling the edit form -- the
// token itself is never sent back to the browser, only re-entered to
// rotate it (see htmxUpdateWebhook).
func githubDispatchDisplayFields(wh *storage.Webhook) (owner, repo string) {
	if wh.Kind != webhooks.KindGitHubDispatch || wh.Config == "" {
		return "", ""
	}
	var cfg webhooks.GitHubDispatchConfig
	_ = json.Unmarshal([]byte(wh.Config), &cfg)
	return cfg.Owner, cfg.Repo
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
		owner, repo := githubDispatchDisplayFields(wh)
		rows = append(rows, webhookRowVM{
			Webhook:     wh,
			Events:      webhookEventOptions(wh.Events),
			IsGitHub:    wh.Kind == webhooks.KindGitHubDispatch,
			GitHubOwner: owner,
			GitHubRepo:  repo,
		})
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

// webhookRequestFromForm reads a submitted create/edit webhook form into a
// webhookRequest, so both HTMX handlers below can share the same
// kind-aware validation/encryption logic as the JSON API
// (Server.buildWebhookFields in handlers_webhooks.go).
func webhookRequestFromForm(r *http.Request) webhookRequest {
	return webhookRequest{
		Kind:        r.FormValue("kind"),
		URL:         r.FormValue("url"),
		Secret:      r.FormValue("secret"),
		Events:      r.Form["events"],
		GitHubOwner: r.FormValue("github_owner"),
		GitHubRepo:  r.FormValue("github_repo"),
		GitHubToken: r.FormValue("github_token"),
	}
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
	req := webhookRequestFromForm(r)
	kind, url, secret, config, problem := s.buildWebhookFields(req, nil)
	if problem != "" {
		redirectWithFlash(w, r, back, problem, "error")
		return
	}
	wh := &storage.Webhook{ID: uuid.NewString(), ProjectID: project.ID, Kind: kind, URL: url, Secret: secret, Config: config, Events: req.Events}
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
	existing, err := s.System.GetWebhook(r.Context(), webhookID)
	if err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	req := webhookRequestFromForm(r)
	kind, url, secret, config, problem := s.buildWebhookFields(req, existing)
	if problem != "" {
		redirectWithFlash(w, r, back, problem, "error")
		return
	}
	wh := &storage.Webhook{ID: webhookID, Kind: kind, URL: url, Secret: secret, Config: config, Events: req.Events}
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
