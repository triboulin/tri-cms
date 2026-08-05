package api

import (
	"net/http"

	"tricms/pkg/auth"
	"tricms/pkg/storage"
)

// htmxProjectLogs renders a project's audit trail (spec: admin-only, but
// scoped to one project rather than a cross-project firehose) -- gated the
// same way as Tokens/Webhooks: global-admin only, reached via a project URL.
func (s *Server) htmxProjectLogs(w http.ResponseWriter, r *http.Request) {
	user, project := s.loadProjectForHTMX(w, r, auth.SectionLogs)
	if user == nil {
		return
	}
	logs, err := s.System.ListLogsByProject(r.Context(), project.ID, 200)
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	data, err := s.buildPageData(r.Context(), user, project, "logs", "", struct{ Logs []*storage.GlobalLog }{logs})
	if err != nil {
		s.htmxServerError(w, r)
		return
	}
	s.render(w, "page:project_logs", data)
}
