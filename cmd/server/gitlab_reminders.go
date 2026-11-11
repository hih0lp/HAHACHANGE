package main

import (
	"net/http"
	"strings"
)

// registerGitLabReminderRoute relays the current user's demo identity to the
// GitLab integration. The integration validates it and applies GitLab's own
// project permissions before returning any review follow-up.
func registerGitLabReminderRoute(mux *http.ServeMux, endpoint, project string) {
	if endpoint == "" || project == "" {
		return
	}
	mux.HandleFunc("GET /v1/gitlab/reminders", func(w http.ResponseWriter, r *http.Request) {
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || bearer == "" {
			writeError(w, http.StatusUnauthorized, errText("team session required"))
			return
		}
		result, err := mcpTeamTool(r.Context(), endpoint, "", bearer, "gitlab_review_reminders", map[string]any{"project": project})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, errText("GitLab reminders unavailable"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})
}
