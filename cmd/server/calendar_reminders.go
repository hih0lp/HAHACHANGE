package main

import (
	"net/http"
	"strings"
	"time"
)

// registerCalendarReminderRoute reads the editable mentor calendar mock
// through the caller's team session. The mock enforces the subject binding.
func registerCalendarReminderRoute(mux *http.ServeMux, endpoint string) {
	if endpoint == "" {
		return
	}
	mux.HandleFunc("GET /v1/calendar/reminders", func(w http.ResponseWriter, r *http.Request) {
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || bearer == "" {
			writeError(w, http.StatusUnauthorized, errText("team session required"))
			return
		}
		moscow, err := time.LoadLocation("Europe/Moscow")
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, errText("calendar timezone unavailable"))
			return
		}
		now := time.Now().In(moscow)
		result, err := mcpTeamTool(r.Context(), endpoint, "", bearer, "calendar_onboarding_events", map[string]any{"time_min": now.Format(time.RFC3339), "time_max": now.Add(24 * time.Hour).Format(time.RFC3339)})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, errText("calendar reminders unavailable"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})
}
