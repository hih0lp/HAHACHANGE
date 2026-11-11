package main

import (
	"net/http"
	"os"

	"github.com/hackchange/avito-rag/internal/scenario"
)

// registerTeamSessionRoute gives the demo chat one server-configured identity.
// The password stays in the server environment; only a short-lived JWT reaches
// this browser session.
func registerTeamSessionRoute(mux *http.ServeMux, api scenario.Client) {
	mux.HandleFunc("POST /v1/team-access/session", func(w http.ResponseWriter, r *http.Request) {
		username := env("TEAM_ACCESS_CHAT_USERNAME", "intern")
		password := os.Getenv("TEAM_ACCESS_INTERN_PASSWORD")
		if password == "" {
			writeError(w, http.StatusServiceUnavailable, errText("team access demo identity is unavailable"))
			return
		}
		result, err := api.Login(r.Context(), username, password)
		if err != nil {
			scenarioError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})
}
