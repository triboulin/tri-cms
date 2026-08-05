package api

import (
	"net/http"
	"strconv"

	"tricms/pkg/storage"
)

// handleListLogs returns the most recent audit log entries for one project
// (not a cross-project firehose). Global-admin only (spec §4.2 sidebar:
// "[Logs] visible uniquement si rôle == ADMIN"), mounted under
// /projects/{projectID}/logs alongside Tokens/Webhooks.
func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	project := ProjectFromContext(r.Context())
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	logs, err := s.System.ListLogsByProject(r.Context(), project.ID, limit)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	if logs == nil {
		logs = []*storage.GlobalLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}
