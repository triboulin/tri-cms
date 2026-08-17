package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tricms/pkg/auth"
	trischema "tricms/pkg/schema"
	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
)

// ---- View models -------------------------------------------------------

// SelectOption is one <option> in a Media/Reference picker. PreviewURL/
// IsImage/IsVideo are only populated for Media-type options, so the content
// form can render a thumbnail-grid picker modal instead of a bare <select>
// (choosing a media by filename alone, with dozens of similarly-named
// uploads, was effectively guesswork).
type SelectOption struct {
	Value      string
	Label      string
	Selected   bool
	PreviewURL string
	IsImage    bool
	IsVideo    bool
}

// ContentFieldVM is the view-model for one dynamically rendered form field
// on the content create/edit page. Kind drives which HTML control the
// template renders (see computeInputKind below).
type ContentFieldVM struct {
	Key           string
	Label         string
	Kind          string
	Required      bool
	Placeholder   string
	ValueStr      string
	Checked       bool
	Options       []string
	SelectOptions []SelectOption
	Lat           string
	Lng           string
}

func computeInputKind(f trischema.Field) string {
	if f.Cardinality == trischema.Collection {
		switch f.Type {
		case trischema.Media:
			return "media-multiselect"
		case trischema.Reference:
			return "reference-multiselect"
		default:
			return "collection-scalar"
		}
	}
	switch f.Type {
	case trischema.RichTextMD:
		return "richtext-md"
	case trischema.RichTextHTML:
		return "richtext-html"
	case trischema.Float:
		return "number-float"
	case trischema.Int:
		return "number-int"
	case trischema.Date:
		return "date"
	case trischema.Boolean:
		return "checkbox"
	case trischema.Enum:
		return "enum"
	case trischema.URL:
		return "url"
	case trischema.Color:
		return "color"
	case trischema.JSONType:
		return "json"
	case trischema.GeoPoint:
		return "geopoint"
	case trischema.Media:
		return "media-select"
	case trischema.Reference:
		return "reference-select"
	default: // Text, Slug
		return "text"
	}
}

// referenceLabel builds a human-friendly label for a Reference/Media picker
// option by looking for a conventional "display" field in the target
// content's data, falling back to its raw id.
func referenceLabel(data map[string]any, id string) string {
	for _, key := range []string{"title", "name", "label"} {
		if v, ok := data[key].(string); ok && v != "" {
			return v
		}
	}
	return id
}

// buildContentFieldVMs prepares the dynamic form's view-models. existing is
// nil when creating a new content instance.
func (s *Server) buildContentFieldVMs(r *http.Request, projectID string, db *storage.ProjectDB, def *trischema.Definition, existing map[string]any) ([]ContentFieldVM, error) {
	var mediaOptions []SelectOption
	referenceOptionsCache := map[string][]SelectOption{}

	vms := make([]ContentFieldVM, 0, len(def.Fields))
	for _, f := range def.Fields {
		kind := computeInputKind(f)
		vm := ContentFieldVM{Key: f.Key, Label: f.Label, Kind: kind, Required: f.Required, Placeholder: f.Placeholder, Options: f.Options}
		if vm.Label == "" {
			vm.Label = f.Key
		}

		var raw any
		if existing != nil {
			raw = existing[f.Key]
		}

		switch kind {
		case "media-select", "media-multiselect":
			if mediaOptions == nil {
				medias, err := db.ListMedias(r.Context())
				if err != nil {
					return nil, err
				}
				mediaOptions = make([]SelectOption, 0, len(medias))
				for _, m := range medias {
					mediaOptions = append(mediaOptions, SelectOption{
						Value:      m.ID,
						Label:      m.Filename,
						PreviewURL: "/projects/" + projectID + "/medias/" + m.ID + "/file",
						IsImage:    strings.HasPrefix(m.MimeType, "image/"),
						IsVideo:    strings.HasPrefix(m.MimeType, "video/"),
					})
				}
			}
			vm.SelectOptions = append([]SelectOption(nil), mediaOptions...)
		case "reference-select", "reference-multiselect":
			opts, ok := referenceOptionsCache[f.TargetSchema]
			if !ok {
				contents, err := db.ListContents(r.Context(), f.TargetSchema)
				if err != nil {
					return nil, err
				}
				opts = make([]SelectOption, 0, len(contents))
				for _, c := range contents {
					var data map[string]any
					_ = json.Unmarshal([]byte(c.Data), &data)
					opts = append(opts, SelectOption{Value: c.ID, Label: referenceLabel(data, c.ID)})
				}
				referenceOptionsCache[f.TargetSchema] = opts
			}
			vm.SelectOptions = append([]SelectOption(nil), opts...)
		}

		switch kind {
		case "checkbox":
			b, _ := raw.(bool)
			vm.Checked = b
		case "number-float", "number-int":
			if raw != nil {
				vm.ValueStr = trimFloat(raw)
			}
		case "geopoint":
			if obj, ok := raw.(map[string]any); ok {
				vm.Lat = trimFloat(obj["lat"])
				vm.Lng = trimFloat(obj["lng"])
			}
		case "json":
			if raw != nil {
				b, _ := json.MarshalIndent(raw, "", "  ")
				vm.ValueStr = string(b)
			}
		case "media-select", "reference-select", "enum":
			if v, ok := raw.(string); ok {
				vm.ValueStr = v
				for i := range vm.SelectOptions {
					if vm.SelectOptions[i].Value == v {
						vm.SelectOptions[i].Selected = true
					}
				}
			}
		case "media-multiselect", "reference-multiselect":
			selected := map[string]bool{}
			if arr, ok := raw.([]any); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						selected[s] = true
					}
				}
			}
			for i := range vm.SelectOptions {
				if selected[vm.SelectOptions[i].Value] {
					vm.SelectOptions[i].Selected = true
				}
			}
		case "collection-scalar":
			if arr, ok := raw.([]any); ok {
				parts := make([]string, 0, len(arr))
				for _, v := range arr {
					parts = append(parts, fmt.Sprintf("%v", v))
				}
				vm.ValueStr = strings.Join(parts, ", ")
			}
		default: // text, textarea, url, color, date
			if v, ok := raw.(string); ok {
				vm.ValueStr = v
			}
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

func trimFloat(v any) string {
	f, ok := v.(float64)
	if !ok {
		return ""
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseContentDataFromForm turns posted form values back into the
// map[string]any shape trischema.ValidateAndNormalize expects (mirroring
// JSON decoding). Only non-empty values are set; leaving a field absent
// lets the existing required/default-collection logic in pkg/schema apply
// uniformly, so this function does not need to duplicate that logic.
//
// It is best-effort: a parse error on one field (e.g. invalid JSON, a
// non-numeric coordinate) is recorded but does not stop the other fields
// from being collected, so the caller can re-render the form with
// everything the user typed intact plus a single error message, instead of
// discarding the whole submission over one bad field.
func parseContentDataFromForm(r *http.Request, def *trischema.Definition) (map[string]any, error) {
	data := map[string]any{}
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, f := range def.Fields {
		kind := computeInputKind(f)
		switch kind {
		case "checkbox":
			data[f.Key] = r.PostForm.Get(f.Key) == "true"

		case "geopoint":
			latStr, lngStr := r.PostForm.Get(f.Key+"__lat"), r.PostForm.Get(f.Key+"__lng")
			if latStr == "" && lngStr == "" {
				continue
			}
			lat, err1 := strconv.ParseFloat(latStr, 64)
			lng, err2 := strconv.ParseFloat(lngStr, 64)
			if err1 != nil || err2 != nil {
				fail(fmt.Errorf("champ %q : latitude/longitude invalides", f.Key))
				continue
			}
			data[f.Key] = map[string]any{"lat": lat, "lng": lng}

		case "json":
			raw := strings.TrimSpace(r.PostForm.Get(f.Key))
			if raw == "" {
				continue
			}
			var v any
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				fail(fmt.Errorf("champ %q : JSON invalide (%v)", f.Key, err))
				continue
			}
			data[f.Key] = v

		case "media-multiselect", "reference-multiselect":
			values := r.PostForm[f.Key]
			if len(values) == 0 {
				continue
			}
			arr := make([]any, 0, len(values))
			for _, v := range values {
				arr = append(arr, v)
			}
			data[f.Key] = arr

		case "collection-scalar":
			raw := strings.TrimSpace(r.PostForm.Get(f.Key))
			if raw == "" {
				continue
			}
			parts := strings.Split(raw, ",")
			arr := make([]any, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				arr = append(arr, coerceScalar(f.Type, p))
			}
			data[f.Key] = arr

		case "number-float", "number-int":
			raw := strings.TrimSpace(r.PostForm.Get(f.Key))
			if raw == "" {
				continue
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				fail(fmt.Errorf("champ %q : nombre invalide", f.Key))
				continue
			}
			data[f.Key] = v

		default:
			raw := r.PostForm.Get(f.Key)
			if raw == "" {
				continue
			}
			data[f.Key] = raw
		}
	}
	return data, firstErr
}

// coerceScalar converts one comma-separated element of a "collection-scalar"
// input into the Go type trischema.ValidateAndNormalize expects for t.
func coerceScalar(t trischema.FieldType, raw string) any {
	switch t {
	case trischema.Float, trischema.Int:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return raw // let ValidateAndNormalize surface the type error
		}
		return f
	case trischema.Boolean:
		return raw == "true"
	default:
		return raw
	}
}

// ---- Handlers -----------------------------------------------------------

type contentListViewData struct {
	Schema      *storage.Schema
	Fields      []trischema.Field
	Contents    []contentRowVM
	HasRichText bool // whether to load the Quill/EasyMDE assets for the row-edit modal
}

type contentRowVM struct {
	ID        string
	Status    storage.ContentStatus
	Cells     []contentCellVM
	CreatedAt string
}

// contentCellVM is one table cell in the content list. Media fields render
// as small thumbnails (the file itself, not its id/name -- a filename tells
// an editor scanning the table nothing, the picture does) via MediaURLs;
// every other field renders as truncated text via Text.
type contentCellVM struct {
	Text      string
	MediaURLs []string
}

// mediaThumbURLs extracts one or more "/projects/.../medias/{id}/file" URLs
// from a Media field's raw value, which is a single id string (Simple
// cardinality) or a JSON array of id strings (Collection). Missing/blank
// ids are skipped rather than producing a broken <img>.
func mediaThumbURLs(projectID string, raw any) []string {
	ids := make([]string, 0, 1)
	switch t := raw.(type) {
	case string:
		if t != "" {
			ids = append(ids, t)
		}
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				ids = append(ids, s)
			}
		}
	}
	urls := make([]string, 0, len(ids))
	for _, id := range ids {
		urls = append(urls, "/projects/"+projectID+"/medias/"+id+"/file")
	}
	return urls
}

func (s *Server) htmxContentList(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
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
		writeHTMXStorageError(w, r, err, "/projects/"+project.ID)
		return
	}
	items, err := db.ListContents(r.Context(), slug)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}

	// Reference fields store the target content's id, not anything a human
	// can read -- resolve each distinct target schema's ids to labels once
	// (referenceLabel, the same helper the edit form's picker already uses)
	// rather than per row/field.
	referenceLabelCache := map[string]map[string]string{}
	referenceLabels := func(targetSchema string) (map[string]string, error) {
		if labels, ok := referenceLabelCache[targetSchema]; ok {
			return labels, nil
		}
		targets, err := db.ListContents(r.Context(), targetSchema)
		if err != nil {
			return nil, err
		}
		labels := make(map[string]string, len(targets))
		for _, t := range targets {
			var tdata map[string]any
			_ = json.Unmarshal([]byte(t.Data), &tdata)
			labels[t.ID] = referenceLabel(tdata, t.ID)
		}
		referenceLabelCache[targetSchema] = labels
		return labels, nil
	}

	rows := make([]contentRowVM, 0, len(items))
	for _, c := range items {
		var data map[string]any
		_ = json.Unmarshal([]byte(c.Data), &data)
		cells := make([]contentCellVM, 0, len(def.Fields))
		for _, f := range def.Fields {
			switch f.Type {
			case trischema.Media:
				cells = append(cells, contentCellVM{MediaURLs: mediaThumbURLs(project.ID, data[f.Key])})
			case trischema.Reference:
				labels, err := referenceLabels(f.TargetSchema)
				if err != nil {
					s.htmxServerError(w, r)
					return
				}
				cells = append(cells, contentCellVM{Text: formatCell(resolveReferenceLabels(data[f.Key], labels))})
			case trischema.RichTextMD, trischema.RichTextHTML:
				raw, _ := data[f.Key].(string)
				cells = append(cells, contentCellVM{Text: formatCell(stripMarkupPreview(f.Type, raw))})
			default:
				cells = append(cells, contentCellVM{Text: formatCell(data[f.Key])})
			}
		}
		rows = append(rows, contentRowVM{ID: c.ID, Status: c.Status, Cells: cells, CreatedAt: c.CreatedAt.Format("2006-01-02 15:04")})
	}

	hasRichText := false
	for _, f := range def.Fields {
		if f.Type == trischema.RichTextMD || f.Type == trischema.RichTextHTML {
			hasRichText = true
			break
		}
	}
	content := contentListViewData{Schema: sc, Fields: def.Fields, Contents: rows, HasRichText: hasRichText}
	data, err := s.buildPageData(r.Context(), user, project, "collections", sc.Name, content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	applyFlash(r, data)
	s.render(w, "page:content_list", data)
}

// resolveReferenceLabels swaps a Reference field's raw stored id(s) for the
// target content's display label, using the same id->label map
// referenceLabels builds per target schema. Shape-preserving (string stays
// a string, []any stays a []any) so the result can go straight through
// formatCell like any other field's value; ids with no matching label
// (e.g. a since-deleted target) fall back to the raw id rather than
// disappearing silently.
func resolveReferenceLabels(raw any, labels map[string]string) any {
	switch t := raw.(type) {
	case string:
		if label, ok := labels[t]; ok {
			return label
		}
		return t
	case []any:
		resolved := make([]any, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				if label, ok := labels[s]; ok {
					resolved = append(resolved, label)
					continue
				}
			}
			resolved = append(resolved, item)
		}
		return resolved
	default:
		return raw
	}
}

// cellTextLimit caps how much of a cell's value the content-list table
// shows: rows are meant for scanning many records at a glance, not reading
// full field values (the row-edit modal has the untruncated data).
const cellTextLimit = 64

var (
	mdLinkRe       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdListMarkerRe = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+\.)\s+`)
	mdMarkerRe     = regexp.MustCompile("[*_`#>]+")
	htmlTagRe      = regexp.MustCompile(`<[^>]*>`)
	whitespaceRe   = regexp.MustCompile(`\s+`)
)

// stripMarkupPreview turns a RichText_MD/RichText_HTML field's raw value
// into a plain-text snippet for the collection list table: without this, the
// table showed literal "**bold**"/"<p>…</p>" syntax instead of readable text,
// making the list noisy to scan.
func stripMarkupPreview(t trischema.FieldType, s string) string {
	if s == "" {
		return s
	}
	switch t {
	case trischema.RichTextHTML:
		s = htmlTagRe.ReplaceAllString(s, " ")
	case trischema.RichTextMD:
		s = mdLinkRe.ReplaceAllString(s, "$1")
		s = mdListMarkerRe.ReplaceAllString(s, "")
		s = mdMarkerRe.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(s, " "))
}

func formatCell(v any) string {
	var s string
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		s = t
	case bool:
		if t {
			return "✓"
		}
		return "—"
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		s = strings.Join(parts, ", ")
	default:
		s = fmt.Sprintf("%v", t)
	}
	if len(s) > cellTextLimit {
		s = s[:cellTextLimit] + "…"
	}
	return s
}

type contentFormViewData struct {
	Schema      *storage.Schema
	ContentID   string // empty when creating
	Fields      []ContentFieldVM
	Status      storage.ContentStatus
	HasRichText bool // true when a RichText_MD/RichText_HTML editor needs to be loaded
}

// htmxContentForm serves both the "new" and "edit" routes: it renders the
// same dynamic form, prefilled when a contentID is present.
func (s *Server) htmxContentForm(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
	if user == nil {
		return
	}
	slug := chi.URLParam(r, "schemaSlug")
	contentID := chi.URLParam(r, "contentID")

	db, err := s.projectDB(project.ID)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	sc, def, err := s.loadSchemaDefinition(r.Context(), db, slug)
	if err != nil {
		writeHTMXStorageError(w, r, err, "/projects/"+project.ID)
		return
	}

	var existing map[string]any
	status := storage.StatusDraft
	if contentID != "" {
		c, err := db.GetContent(r.Context(), contentID)
		if err != nil {
			writeHTMXStorageError(w, r, err, "/projects/"+project.ID+"/schemas/"+slug+"/contents")
			return
		}
		_ = json.Unmarshal([]byte(c.Data), &existing)
		status = c.Status
	}
	s.renderContentForm(w, r, user, project, db, sc, def, contentID, existing, status, "", "")
}

// renderContentForm builds and renders the content create/edit form.
// existing/status let a failed create/update POST re-display exactly what
// the user typed (via the best-effort map parseContentDataFromForm always
// returns) alongside an inline error, instead of redirecting to a blank or
// stale form and discarding the submission.
func (s *Server) renderContentForm(w http.ResponseWriter, r *http.Request, user *storage.User, project *storage.Project, db *storage.ProjectDB, sc *storage.Schema, def *trischema.Definition, contentID string, existing map[string]any, status storage.ContentStatus, flash, flashKind string) {
	fields, err := s.buildContentFieldVMs(r, project.ID, db, def, existing)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	hasRichText := false
	for _, f := range fields {
		if f.Kind == "richtext-md" || f.Kind == "richtext-html" {
			hasRichText = true
			break
		}
	}
	title := "Nouveau contenu · " + sc.Name
	if contentID != "" {
		title = "Modifier · " + sc.Name
	}
	content := contentFormViewData{Schema: sc, ContentID: contentID, Fields: fields, Status: status, HasRichText: hasRichText}
	data, err := s.buildPageData(r.Context(), user, project, "collections", title, content)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	// Override the generic "back to Collections root" default: the form's
	// actual parent is this specific schema's content list, not the list
	// of every schema in the project.
	data.BackLabel = sc.Name
	data.BackURL = "/projects/" + project.ID + "/schemas/" + sc.Slug + "/contents"
	if flash != "" {
		data.Flash, data.FlashKind = flash, flashKind
	} else {
		applyFlash(r, data)
	}
	s.render(w, "page:content_form", data)
}

func (s *Server) htmxCreateContent(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
	if user == nil {
		return
	}
	slug := chi.URLParam(r, "schemaSlug")
	listPath := "/projects/" + project.ID + "/schemas/" + slug + "/contents"
	formPath := listPath + "/new"

	if err := r.ParseForm(); err != nil {
		// Nothing usable to redisplay: the form body itself didn't parse.
		redirectWithFlash(w, r, formPath, "Formulaire invalide.", "error")
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, formPath, "Erreur serveur.", "error")
		return
	}
	sc, def, err := s.loadSchemaDefinition(r.Context(), db, slug)
	if err != nil {
		writeHTMXStorageError(w, r, err, listPath)
		return
	}

	status := storage.StatusDraft
	if r.FormValue("status") == string(storage.StatusPublished) {
		status = storage.StatusPublished
	}

	raw, err := parseContentDataFromForm(r, def)
	fail := func(message string) {
		s.renderContentForm(w, r, user, project, db, sc, def, "", raw, status, message, "error")
	}
	if err != nil {
		fail(err.Error())
		return
	}
	normalized, err := trischema.ValidateAndNormalize(def, slug, raw, "", s.schemaHooks(r.Context(), db, def, ""))
	if err != nil {
		fail("Validation : " + err.Error())
		return
	}
	now := jsonTimestamp()
	normalized["created_at"] = now
	normalized["updated_at"] = now

	dataJSON, err := json.Marshal(normalized)
	if err != nil {
		fail("Erreur d'encodage.")
		return
	}
	c := &storage.Content{ID: uuid.NewString(), SchemaSlug: slug, Data: string(dataJSON), Status: status}
	if err := db.CreateContent(r.Context(), c); err != nil {
		fail("Création impossible : " + err.Error())
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "content.create", map[string]string{"schema": slug, "id": c.ID})
	s.dispatchWebhooksAsync(r.Context(), project.ID, webhooks.EventContentUpdate, map[string]string{"id": c.ID, "schema": slug})

	http.Redirect(w, r, listPath, http.StatusSeeOther)
}

func (s *Server) htmxUpdateContent(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
	if user == nil {
		return
	}
	slug := chi.URLParam(r, "schemaSlug")
	contentID := chi.URLParam(r, "contentID")
	listPath := "/projects/" + project.ID + "/schemas/" + slug + "/contents"
	formPath := listPath + "/" + contentID + "/edit"

	if err := r.ParseForm(); err != nil {
		redirectWithFlash(w, r, formPath, "Formulaire invalide.", "error")
		return
	}
	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, formPath, "Erreur serveur.", "error")
		return
	}
	existing, err := db.GetContent(r.Context(), contentID)
	if err != nil {
		writeHTMXStorageError(w, r, err, listPath)
		return
	}
	sc, def, err := s.loadSchemaDefinition(r.Context(), db, slug)
	if err != nil {
		writeHTMXStorageError(w, r, err, listPath)
		return
	}

	status := existing.Status
	if r.FormValue("status") == string(storage.StatusPublished) {
		status = storage.StatusPublished
	} else if r.FormValue("status") == string(storage.StatusDraft) {
		status = storage.StatusDraft
	}

	raw, err := parseContentDataFromForm(r, def)
	fail := func(message string) {
		s.renderContentForm(w, r, user, project, db, sc, def, contentID, raw, status, message, "error")
	}
	if err != nil {
		fail(err.Error())
		return
	}
	normalized, err := trischema.ValidateAndNormalize(def, slug, raw, contentID, s.schemaHooks(r.Context(), db, def, contentID))
	if err != nil {
		fail("Validation : " + err.Error())
		return
	}

	var previous map[string]any
	_ = json.Unmarshal([]byte(existing.Data), &previous)
	if createdAt, ok := previous["created_at"]; ok {
		normalized["created_at"] = createdAt
	}
	normalized["updated_at"] = jsonTimestamp()

	dataJSON, err := json.Marshal(normalized)
	if err != nil {
		fail("Erreur d'encodage.")
		return
	}
	existing.Data = string(dataJSON)
	existing.Status = status
	if err := db.UpdateContent(r.Context(), existing); err != nil {
		fail("Mise à jour impossible : " + err.Error())
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "content.update", map[string]string{"schema": slug, "id": contentID})
	s.dispatchWebhooksAsync(r.Context(), project.ID, webhooks.EventContentUpdate, map[string]string{"id": contentID, "schema": slug})

	http.Redirect(w, r, listPath, http.StatusSeeOther)
}

func (s *Server) htmxDeleteContent(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
	if user == nil {
		return
	}
	slug := chi.URLParam(r, "schemaSlug")
	contentID := chi.URLParam(r, "contentID")
	listPath := "/projects/" + project.ID + "/schemas/" + slug + "/contents"

	_ = r.ParseForm()
	force := r.FormValue("force") == "true"

	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, listPath, "Erreur serveur.", "error")
		return
	}
	if !force {
		n, err := db.CountContentsReferencing(r.Context(), contentID)
		if err != nil {
			redirectWithFlash(w, r, listPath, "Erreur serveur.", "error")
			return
		}
		if n > 0 {
			redirectWithFlash(w, r, listPath,
				"Ce contenu est référencé par d'autres contenus. Utilisez « Forcer la suppression » pour confirmer.", "error")
			return
		}
	}
	if err := db.DeleteContent(r.Context(), contentID); err != nil {
		redirectWithFlash(w, r, listPath, "Suppression impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "content.delete", map[string]string{"schema": slug, "id": contentID})
	s.dispatchWebhooksAsync(r.Context(), project.ID, webhooks.EventContentUpdate, map[string]string{"id": contentID, "schema": slug})
	http.Redirect(w, r, listPath, http.StatusSeeOther)
}

func (s *Server) htmxToggleContentStatus(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionCollections)
	if user == nil {
		return
	}
	slug := chi.URLParam(r, "schemaSlug")
	contentID := chi.URLParam(r, "contentID")
	listPath := "/projects/" + project.ID + "/schemas/" + slug + "/contents"

	db, err := s.projectDB(project.ID)
	if err != nil {
		redirectWithFlash(w, r, listPath, "Erreur serveur.", "error")
		return
	}
	c, err := db.GetContent(r.Context(), contentID)
	if err != nil {
		writeHTMXStorageError(w, r, err, listPath)
		return
	}
	if c.Status == storage.StatusPublished {
		c.Status = storage.StatusDraft
	} else {
		c.Status = storage.StatusPublished
	}
	if err := db.UpdateContent(r.Context(), c); err != nil {
		redirectWithFlash(w, r, listPath, "Mise à jour impossible : "+err.Error(), "error")
		return
	}
	_ = s.System.LogAction(r.Context(), user.ID, project.ID, "content.update", map[string]string{"schema": slug, "id": contentID, "status": string(c.Status)})
	s.dispatchWebhooksAsync(r.Context(), project.ID, webhooks.EventContentUpdate, map[string]string{"id": contentID, "schema": slug})
	http.Redirect(w, r, listPath, http.StatusSeeOther)
}

func jsonTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
