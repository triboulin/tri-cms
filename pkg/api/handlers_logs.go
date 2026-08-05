package api

import (
	"net/http"
	"strconv"
)

// handleListLogs returns the most recent global audit log entries.
// Global-admin only (spec §4.2 sidebar: "[Logs] visible uniquement si
// rôle == ADMIN").
func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	logs, err := s.System.ListLogs(r.Context(), limit)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}
