package main

import (
	"net/http"
	"strings"
	"time"
)

// registerCalendarReminderRoute reads only the selected onboarding calendar
// through the caller's team session. The calendar integration owns the Google
// credential and enforces the subject binding.
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
		now := time.Now().UTC()
		result, err := mcpTeamTool(r.Context(), endpoint, "", bearer, "calendar_onboarding_events", map[string]any{"time_min": now.Format(time.RFC3339), "time_max": now.Add(24 * time.Hour).Format(time.RFC3339)})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, errText("calendar reminders unavailable"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})
}
