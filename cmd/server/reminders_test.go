package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRemindersOnlyForAssignedOpenTasksInsideWindow(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" || !strings.Contains(r.URL.Query().Get("jql"), "duedate is not EMPTY") {
			t.Errorf("unexpected Jira request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"SCRUM-1","fields":{"summary":"Моя задача","duedate":"2026-11-10","assignee":{"accountId":"intern-1"},"status":{"statusCategory":{"key":"indeterminate"}}}},
			{"key":"SCRUM-2","fields":{"summary":"Чужая задача","duedate":"2026-11-10","assignee":{"accountId":"other"},"status":{"statusCategory":{"key":"indeterminate"}}}},
			{"key":"SCRUM-3","fields":{"summary":"Готово","duedate":"2026-11-10","assignee":{"accountId":"intern-1"},"status":{"statusCategory":{"key":"done"}}}},
			{"key":"SCRUM-4","fields":{"summary":"Позже","duedate":"2026-11-12","assignee":{"accountId":"intern-1"},"status":{"statusCategory":{"key":"new"}}}}
		]}`))
	}))
	defer jira.Close()
	cfg := reminderConfig{InternAccountID: "intern-1", HoursBefore: 24, TimeZone: "Europe/Moscow"}
	jcfg := jiraConfig{Mode: "live", BaseURL: jira.URL, ProjectKey: "SCRUM"}
	zone, _ := time.LoadLocation(cfg.TimeZone)
	before := time.Date(2026, 11, 9, 23, 0, 0, 0, zone)
	result, err := loadReminders(context.Background(), jcfg, cfg, before)
	if err != nil || len(result.Reminders) != 0 {
		t.Fatalf("before threshold: %+v %v", result, err)
	}
	inside := before.Add(2 * time.Hour)
	result, err = loadReminders(context.Background(), jcfg, cfg, inside)
	if err != nil || len(result.Reminders) != 1 || result.Reminders[0].Key != "SCRUM-1" || result.Reminders[0].DueAt.Day() != 11 {
		t.Fatalf("inside window: %+v %v", result, err)
	}
	after := time.Date(2026, 11, 11, 0, 0, 0, 0, zone)
	result, err = loadReminders(context.Background(), jcfg, cfg, after)
	if err != nil || len(result.Reminders) != 0 {
		t.Fatalf("after deadline: %+v %v", result, err)
	}
	cfg.HoursBefore = 48
	result, err = loadReminders(context.Background(), jcfg, cfg, time.Date(2026, 11, 8, 23, 0, 0, 0, zone))
	if err != nil || len(result.Reminders) != 0 {
		t.Fatalf("before 48h: %+v %v", result, err)
	}
	result, err = loadReminders(context.Background(), jcfg, cfg, time.Date(2026, 11, 9, 1, 0, 0, 0, zone))
	if err != nil || len(result.Reminders) != 1 {
		t.Fatalf("inside 48h: %+v %v", result, err)
	}
	due := result.Reminders[0].DueAt
	if mentorWindow(due.Add(-25*time.Hour), due) || !mentorWindow(due.Add(-24*time.Hour), due) || mentorWindow(due, due) {
		t.Fatal("mentor 24h boundary")
	}
}

func TestReminderRoutesAndDisabledAccessRoute(t *testing.T) {
	mux := http.NewServeMux()
	registerReminderRoutes(mux, "secret", jiraConfig{Mode: "off"}, reminderConfig{})
	if r := call(mux, "GET", "/v1/reminders", "", ""); r.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized reminder read = %d", r.Code)
	}
	r := call(mux, "GET", "/v1/reminders", "", "secret")
	var result reminderSnapshot
	if r.Code != http.StatusOK || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Enabled || result.Message == "" {
		t.Fatalf("disabled reminders: %d %s", r.Code, r.Body.String())
	}
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/v1/reminders/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"api_key": "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&result); err != nil || result.Enabled || result.Message == "" {
		t.Fatalf("websocket reminder snapshot: %+v %v", result, err)
	}
}
