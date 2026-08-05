package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"tricms/pkg/auth"
	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
)

type conceptionViewData struct {
	Schemas          []*storage.Schema
	AvailableSchemas []string
	NewFieldRowCtxs  []FieldRowRenderCtx
	NewSchemaSlug    string
	NewSchemaName    string
}

// htmxConception renders the schema management page (spec §4.2 "Conception",
// CONCEPTEUR+ only): a create-schema form with the dynamic field builder
// pre-seeded with one empty row, and the list of existing schemas.
func (s *Server) htmxConception(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionConception)
	if user == nil {
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	s.renderConceptionPage(w, r, user, project, db, nil, "", "", "", "")
}

// renderConceptionPage builds and renders the Conception page. newRows/
// newSlug/newName let a failed schema-creation POST re-display the exact
// values the user typed (plus an inline error) instead of redirecting to a
// blank form -- the previous behavior silently discarded everything the
// user had entered, including the dynamic field rows.
func (s *Server) renderConceptionPage(w http.ResponseWriter, r *http.Request, user *storage.User, project *storage.Project, db *storage.ProjectDB, newRows []FieldRowVM, newSlug, newName, flash, flashKind string) {
	schemas, err := db.ListSchemas(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	slugs := make([]string, 0, len(schemas))
	for _, sc := range schemas {
		slugs = append(slugs, sc.Slug)
	}
	if newRows == nil {
		newRows = []FieldRowVM{{Index: 0}}
	}

	content := conceptionViewData{
		Schemas:          schemas,
		AvailableSchemas: slugs,
		NewFieldRowCtxs:  buildFieldRowCtxs(newRows, slugs),
		NewSchemaSlug:    newSlug,
		NewSchemaName:    newName,
	}
	data, err := s.buildPageData(r.Context(), user, project, "conception", "", content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	if flash != "" {
		data.Flash, data.FlashKind = flash, flashKind
	} else {
		applyFlash(r, data)
	}
	s.render(w, "page:conception", data)
}

func (s *Server) htmxCreateSchema(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionConception)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/conception"
	if err := r.ParseForm(); err != nil {
		// Nothing usable to redisplay: the form body itself didn't parse.
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}

	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, back, "Erreur serveur.", "error")
		return
	}

	slug := r.FormValue("slug")
	name := r.FormValue("name")
	fields := parseFieldsFromForm(r)
	rows := fieldsToRowVMs(fields)

	fail := func(message string) {
		s.renderConceptionPage(w, r, user, project, db, rows, slug, name, message, "error")
	}

	if slug == "" || name == "" {
		fail("Le slug et le nom du schéma sont requis.")
		return
	}

	def := trischema.Definition{Fields: fields}
	if err := def.Validate(); err != nil {
		fail("Définition invalide : " + err.Error())
		return
	}
	defJSON, err := json.Marshal(def)
	if err != nil {
		fail("Erreur d'encodage de la définition.")
		return
	}

	sc := &storage.Schema{Slug: slug, Name: name, Definition: string(defJSON)}
	if err := db.CreateSchema(r.Context(), sc); err != nil {
		fail("Impossible de créer le schéma : " + err.Error())
		return
	}
	redirectWithFlash(w, r, back, "Schéma « "+name+" » créé.", "success")
}

func (s *Server) htmxEditSchemaPage(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionConception)
	if user == nil {
		return
	}
	slug := chi.URLParam(r, "schemaSlug")
	db, err := s.projectDB(project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	sc, def, err := s.loadSchemaDefinition(r.Context(), db, slug)
	if err != nil {
		writeHTMXStorageError(w, r, err, "/projects/"+project.ID+"/conception")
		return
	}
	s.renderSchemaEditPage(w, r, user, project, db, sc, fieldsToRowVMs(def.Fields), sc.Name, "", "")
}

// renderSchemaEditPage builds and renders the schema-edit page. rows/name
// let a failed update POST re-display exactly what the user submitted
// (including the dynamic field rows) alongside an inline error, instead of
// redirecting back to a GET that reloads the old stored definition and
// silently discards the edit.
func (s *Server) renderSchemaEditPage(w http.ResponseWriter, r *http.Request, user *storage.User, project *storage.Project, db *storage.ProjectDB, sc *storage.Schema, rows []FieldRowVM, name, flash, flashKind string) {
	allSchemas, err := db.ListSchemas(r.Context())
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	slugs := make([]string, 0, len(allSchemas))
	for _, other := range allSchemas {
		if other.Slug != sc.Slug {
			slugs = append(slugs, other.Slug)
		}
	}

	displaySchema := *sc
	displaySchema.Name = name

	content := struct {
		Schema           *storage.Schema
		AvailableSchemas []string
		FieldRowCtxs     []FieldRowRenderCtx
	}{
		Schema:           &displaySchema,
		AvailableSchemas: slugs,
		FieldRowCtxs:     buildFieldRowCtxs(rows, slugs),
	}
	data, err := s.buildPageData(r.Context(), user, project, "conception", "Modifier « "+sc.Name+" »", content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	if flash != "" {
		data.Flash, data.FlashKind = flash, flashKind
	} else {
		applyFlash(r, data)
	}
	s.render(w, "page:schema_edit", data)
}

func (s *Server) htmxUpdateSchema(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionConception)
	if user == nil {
		return
	}
	slug := chi.URLParam(r, "schemaSlug")
	back := "/projects/" + project.ID + "/schemas/" + slug + "/edit"
	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, back, "Formulaire invalide.", "error")
		return
	}

	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, back, "Erreur serveur.", "error")
		return
	}
	existing, _, err := s.loadSchemaDefinition(r.Context(), db, slug)
	if err != nil {
		writeHTMXStorageError(w, r, err, "/projects/"+project.ID+"/conception")
		return
	}

	name := r.FormValue("name")
	fields := parseFieldsFromForm(r)
	rows := fieldsToRowVMs(fields)

	fail := func(message string) {
		s.renderSchemaEditPage(w, r, user, project, db, existing, rows, name, message, "error")
	}

	if name == "" {
		fail("Le nom est requis.")
		return
	}
	def := trischema.Definition{Fields: fields}
	if err := def.Validate(); err != nil {
		fail("Définition invalide : " + err.Error())
		return
	}
	defJSON, err := json.Marshal(def)
	if err != nil {
		fail("Erreur d'encodage.")
		return
	}
	sc := &storage.Schema{Slug: slug, Name: name, FolderID: existing.FolderID, Definition: string(defJSON)}
	if err := db.UpdateSchema(r.Context(), sc); err != nil {
		fail("Mise à jour impossible : " + err.Error())
		return
	}
	redirectWithFlash(w, r, "/projects/"+project.ID+"/conception", "Schéma « "+name+" » mis à jour.", "success")
}

func (s *Server) htmxDeleteSchema(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionConception)
	if user == nil {
		return
	}
	back := "/projects/" + project.ID + "/conception"
	slug := chi.URLParam(r, "schemaSlug")
	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, back, "Erreur serveur.", "error")
		return
	}
	if err := db.DeleteSchema(r.Context(), slug); err != nil {
		redirectWithFlash(w, r, back, "Suppression impossible : "+err.Error(), "error")
		return
	}
	redirectWithFlash(w, r, back, "Schéma « "+slug+" » supprimé (contenus associés inclus).", "success")
}

// writeHTMXStorageError maps a storage error to a 404/redirect for HTMX
// GET pages (mirrors writeStorageError used by the JSON API).
func writeHTMXStorageError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	if err == storage.ErrNotFound {
		http.NotFound(w, r)
		return
	}
	redirectWithFlash(w, r, fallback, "Erreur : "+err.Error(), "error")
}
