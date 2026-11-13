package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func registerPersonalJiraRoute(mux *http.ServeMux, key string, jira jiraConfig, cfg reminderConfig, directory scenario.Client) {
	mux.Handle("GET /v1/tasks/jira", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, errText("team session required"))
			return
		}
		profile, err := directory.Profile(r.Context(), token)
		if err != nil {
			scenarioError(w, err)
			return
		}
		personal := cfg
		personal.InternAccountID = profile.JiraAccountID
		if strings.HasPrefix(personal.InternAccountID, "demo-") {
			personal.InternAccountID = ""
		}
		result, err := loadReminders(r.Context(), jira, personal, time.Now())
		if err != nil {
			writeError(w, http.StatusBadGateway, errText("Не удалось проверить задачи Jira"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})))
}
