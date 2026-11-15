package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestDeadlineNotificationScanSeparatesInternsAndMentor(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	due := now.Add(20 * time.Minute)
	t.Setenv("TEAM_ACCESS_INTERN_PASSWORD", "first-password")
	t.Setenv("TEAM_ACCESS_INTERN2_PASSWORD", "second-password")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/login":
			var in struct {
				Username string `json:"username"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			writeJSON(w, 200, map[string]string{"access_token": in.Username})
		case "/v1/me":
			id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			writeJSON(w, 200, scenario.Profile{ID: id, JiraAccountID: "jira-" + id, CurrentTeamID: "team", Teams: []scenario.Team{{ID: "team", MentorID: "approver"}}})
		case "/rest/api/3/search/jql":
			account := "jira-intern"
			key := "TASK-1"
			if strings.Contains(r.URL.Query().Get("jql"), "jira-intern2") {
				account, key = "jira-intern2", "TASK-2"
			}
			writeJSON(w, 200, map[string]any{"issues": []any{map[string]any{"key": key, "fields": map[string]any{"summary": key, "due_at": due.Format(time.RFC3339), "assignee": map[string]string{"accountId": account}}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	directory := scenario.Client{BaseURL: upstream.URL}
	jira := jiraConfig{Mode: "mock", BaseURL: upstream.URL, ProjectKey: "TASK"}
	cfg := reminderConfig{InternMinutesBefore: 30, MentorMinutesBefore: 30, TimeZone: "UTC"}
	if err := scanDeadlineNotifications(ctx, db, jira, cfg, directory, now); err != nil {
		t.Fatal(err)
	}
	if err := scanDeadlineNotifications(ctx, db, jira, cfg, directory, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ recipient, key string }{{"intern", "TASK-1"}, {"intern2", "TASK-2"}} {
		items, err := readDeadlineNotifications(ctx, db, tc.recipient)
		if err != nil || len(items) != 1 || items[0].Key != tc.key {
			t.Fatalf("%s notifications: %+v %v", tc.recipient, items, err)
		}
	}
	mentor, err := readDeadlineNotifications(ctx, db, "approver")
	if err != nil || len(mentor) != 2 {
		t.Fatalf("mentor notifications: %+v %v", mentor, err)
	}
}
