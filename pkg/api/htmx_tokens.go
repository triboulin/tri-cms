package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// tokensPageContent is the Content shape for page:admin_tokens, kept as a
// single named type so every render (plain list, or right after a create
// reveals the plaintext token once) exposes the same fields to the
// template. Using two different anonymous structs here used to make the
// plain GET crash: the template unconditionally reads .Content.RevealedToken,
// which doesn't exist on a struct that was only ever given a Tokens field.
type tokensPageContent struct {
	Tokens        []*storage.APIToken
	RevealedToken string
	RevealedName  string
}

// htmxAdminTokens renders the global API token management page. Tokens
// moved into Administration (spec §1, §4.3): they're no longer per-project,
// and every token minted here is ADMIN-equivalent -- see pkg/auth
// middleware. Global-admin only.
func (s *Server) htmxAdminTokens(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	tokens, err := s.System.ListAPITokens(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "admin_tokens", "", tokensPageContent{Tokens: tokens})
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:admin_tokens", data)
}

func (s *Server) htmxAdminCreateToken(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	back := "/admin/tokens"
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}
	name := r.FormValue("name")
	if name == "" {
		redirectWithFlash(w, r, back, "Le nom du token est requis.", "error")
		return
	}
	plaintext, err := auth.GenerateAPIToken()
	if err != nil {
		redirectWithFlash(w, r, back, "Génération impossible.", "error")
		return
	}
	t := &storage.APIToken{ID: uuid.NewString(), TokenHash: auth.HashAPIToken(plaintext), Name: name}
	if err := s.System.CreateAPIToken(r.Context(), t); err != nil {
		redirectWithFlash(w, r, back, "Création impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), "", "token.create", map[string]string{"name": t.Name})

	// Shown once, rendered directly (never put in a redirect URL / browser
	// history / server logs): the plaintext token is not persisted anywhere
	// after this response.
	tokens, err := s.System.ListAPITokens(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, nil, "admin_tokens", "", tokensPageContent{Tokens: tokens, RevealedToken: plaintext, RevealedName: name})
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	s.render(w, "page:admin_tokens", data)
}

func (s *Server) htmxAdminDeleteToken(w http.ResponseWriter, r *http.Request) {
	user := s.requireHTMXGlobalAdmin(w, r)
	if user == nil {
		return
	}
	back := "/admin/tokens"
	tokenID := chi.URLParam(r, "tokenID")
	if err := s.System.DeleteAPIToken(r.Context(), tokenID); err != nil {
		writeHTMXStorageError(w, r, err, back)
		return
	}
	_ = s.System.LogAction(r.Context(), ActorLogID(r.Context()), "", "token.delete", map[string]string{"id": tokenID})
	redirectWithFlash(w, r, back, "Token révoqué.", "success")
}
