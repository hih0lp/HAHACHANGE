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
	jcfg := jiraConfig{Mode: "mock", BaseURL: jira.URL, ProjectKey: "SCRUM"}
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
	if deadlineWindow(due.Add(-10*time.Minute-time.Nanosecond), due, 10*time.Minute) || !deadlineWindow(due.Add(-10*time.Minute), due, 10*time.Minute) || !deadlineWindow(due.Add(-5*time.Minute), due, 5*time.Minute) || deadlineWindow(due, due, 5*time.Minute) {
		t.Fatal("minute reminder boundaries")
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

func TestMockJiraMinuteReminderWindow(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"TEST-1","url":"https://jira.example/browse/TEST-1","fields":{"summary":"Задача стажёра","duedate":"2026-11-14","assignee":{"accountId":"intern-account"},"status":{"statusCategory":{"key":"indeterminate"}}}},
			{"key":"TEST-2","fields":{"summary":"Закрыта","duedate":"2026-11-14","assignee":{"accountId":"intern-account"},"status":{"statusCategory":{"key":"done"}}}}
		]}`))
	}))
	defer jira.Close()
	cfg := reminderConfig{InternAccountID: "intern-account", TimeZone: "Europe/Moscow"}
	jcfg := jiraConfig{Mode: "mock", BaseURL: jira.URL, ProjectKey: "TEST"}
	zone, err := time.LoadLocation(cfg.TimeZone)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 11, 15, 0, 0, 0, 0, zone)
	for _, test := range []struct {
		at   time.Time
		want int
	}{
		{due.Add(-10*time.Minute - time.Nanosecond), 0},
		{due.Add(-10 * time.Minute), 1},
		{due.Add(-5 * time.Minute), 1},
		{due, 0},
	} {
		result, err := loadRemindersWindow(context.Background(), jcfg, cfg, test.at, 10*time.Minute)
		if err != nil || len(result.Reminders) != test.want {
			t.Fatalf("at %s: %+v, %v", test.at, result, err)
		}
		if test.want == 1 && (result.Reminders[0].Key != "TEST-1" || !result.Reminders[0].DueAt.Equal(due) || result.Reminders[0].URL != "") {
			t.Fatalf("wrong reminder: %+v", result.Reminders)
		}
	}
}
