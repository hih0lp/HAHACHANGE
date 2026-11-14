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
	"github.com/hackchange/avito-rag/internal/scenario"
)

func TestJiraTasksByTeamProfile(t *testing.T) {
	now := time.Now()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me":
			switch r.Header.Get("Authorization") {
			case "Bearer first":
				writeJSON(w, 200, scenario.Profile{ID: "intern", JiraAccountID: "demo-placeholder"})
			case "Bearer second":
				writeJSON(w, 200, scenario.Profile{ID: "intern2", JiraAccountID: "jira-second"})
			case "Bearer mentor":
				writeJSON(w, 200, scenario.Profile{ID: "approver", Teams: []scenario.Team{
					{ID: "team", MentorID: "approver"}, {ID: "other", MentorID: "someone-else"},
				}})
			default:
				w.WriteHeader(http.StatusUnauthorized)
			}
		case "/v1/teams/team/members":
			if r.Header.Get("Authorization") != "Bearer mentor" {
				t.Errorf("Unexpected team lookup for %q", r.Header.Get("Authorization"))
			}
			writeJSON(w, 200, map[string]any{"members": []scenario.Member{
				{ID: "intern", Role: "intern", JiraAccountID: "demo-placeholder"},
				{ID: "intern2", Role: "intern", JiraAccountID: "jira-second"},
				{ID: "developer", Role: "developer", JiraAccountID: "jira-developer"},
			}})
		case "/rest/api/3/search/jql":
			issues := []map[string]any{}
			for account, key := range map[string]string{"jira-first": "DEMO-1", "jira-second": "DEMO-2"} {
				if !strings.Contains(r.URL.Query().Get("jql"), `assignee = "`+account+`"`) {
					continue
				}
				issues = append(issues, map[string]any{"key": key, "fields": map[string]any{
					"summary": key, "due_at": now.Add(time.Hour).Format(time.RFC3339),
					"assignee": map[string]string{"accountId": account},
				}})
			}
			writeJSON(w, 200, map[string]any{"issues": issues})
		default:
			t.Errorf("Unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	mux := http.NewServeMux()
	registerJiraTasks(mux, "", jiraConfig{Mode: "mock", BaseURL: upstream.URL, ProjectKey: "DEMO"},
		reminderConfig{InternAccountID: "jira-first", HoursBefore: 2, TimeZone: "UTC"}, scenario.Client{BaseURL: upstream.URL})
	for token, want := range map[string]string{"first": "DEMO-1", "second": "DEMO-2", "mentor": "DEMO-1,DEMO-2"} {
		t.Run(token, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/tasks/jira", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.AddCookie(&http.Cookie{Name: "buddy_demo_role", Value: "mentor"})
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			var result reminderSnapshot
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != http.StatusOK || !result.Enabled {
				t.Fatalf("Unexpected response %d: %s", rec.Code, rec.Body.String())
			}
			keys := []string{}
			for _, item := range result.Reminders {
				keys = append(keys, item.Key)
			}
			if got := strings.Join(keys, ","); got != want {
				t.Errorf("Got %s, want %s", got, want)
			}
		})
	}
}

func TestRemindersOnlyForAssignedOpenTasksInsideWindow(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" || !strings.Contains(r.URL.Query().Get("jql"), "assignee =") {
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
			{"key":"TEST-1","fields":{"summary":"Задача стажёра","due_at":"2026-11-14T22:15:00+03:00","assignee":{"accountId":"intern-account"},"status":{"statusCategory":{"key":"indeterminate"}}}},
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
	due := time.Date(2026, 11, 14, 22, 15, 0, 0, zone)
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
