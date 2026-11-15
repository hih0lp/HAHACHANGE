package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestDeadlineNotificationsAreScopedToSelectedRole(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	for _, recipient := range []string{"intern", "intern2", "approver"} {
		kind := "intern_deadline"
		if recipient == "approver" {
			kind = "mentor_deadline"
		}
		if _, err := db.Exec(ctx, `INSERT INTO ai_deadline_notifications(recipient_id,kind,issue_key,summary,due_at,issue_url) VALUES($1,$2,$3,$4,$5,$6)`, recipient, kind, "TASK-"+recipient, "Deadline for "+recipient, time.Now().Add(time.Hour), "https://jira.example/"+recipient); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	registerDeadlineNotifications(mux, db, "api-key", jiraConfig{}, reminderConfig{}, scenario.Client{})
	for _, tc := range []struct{ role, own string }{{"intern", "intern"}, {"intern2", "intern2"}, {"mentor", "approver"}} {
		req := httptest.NewRequest(http.MethodGet, "/v1/deadline-notifications", nil)
		req.Header.Set("X-API-Key", "api-key")
		if tc.role != "intern" {
			req.AddCookie(&http.Cookie{Name: "buddy_demo_role", Value: tc.role})
		}
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req.WithContext(context.WithValue(req.Context(), ownerKey{}, "browser")))
		if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"TASK-`+tc.own+`"`) {
			t.Fatalf("%s: %d %s", tc.role, resp.Code, resp.Body.String())
		}
		for _, other := range []string{"intern", "intern2", "approver"} {
			if other != tc.own && strings.Contains(resp.Body.String(), `"TASK-`+other+`"`) {
				t.Fatalf("%s saw %s's deadline", tc.role, other)
			}
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/deadline-notifications", nil)
	request.Header.Set("X-API-Key", "api-key")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous notification read: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/deadline-notifications", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), ownerKey{}, "browser")))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing API key: %d", response.Code)
	}
}

func TestDeadlineWebSocketAuthenticatesBeforeStreaming(t *testing.T) {
	db := testsupport.Postgres(t)
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer signed" {
			http.Error(w, "forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"intern"}`))
	}))
	defer directory.Close()
	mux := http.NewServeMux()
	registerDeadlineNotifications(mux, db, "api-key", jiraConfig{}, reminderConfig{}, scenario.Client{BaseURL: directory.URL})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, "browser")))
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/deadline-notifications/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"api_key": "api-key", "access_token": "signed"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var message struct {
		Notifications []deadlineNotification `json:"notifications"`
	}
	if err := conn.ReadJSON(&message); err != nil || message.Notifications == nil {
		t.Fatalf("websocket notification snapshot: %+v err=%v", message, err)
	}
}
