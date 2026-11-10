package scenario

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The HTTP fixture exists only in tests. Production always uses Client.
func TestAdvanceFromService(t *testing.T) {
	for _, tc := range []struct{ permission, status, widget string }{{"missing", "needs_input", "access_request_form"}, {"pending", "waiting_access", "approval_status"}, {"granted", "ready_for_task", "task_selection"}, {"unknown", "check_unavailable", "retry"}} {
		t.Run(tc.permission, func(t *testing.T) {
			checks := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer user-token" {
					t.Error("missing delegated token")
				}
				switch r.URL.Path {
				case "/v1/teams/team-a/resources":
					json.NewEncoder(w).Encode(map[string]any{"resources": []Resource{{ID: "jira-a", Kind: "jira_project", Name: "Задачи команды"}, {ID: "gitlab-a", Kind: "gitlab_project"}}})
				case "/v1/access/check":
					checks++
					var input map[string]string
					json.NewDecoder(r.Body).Decode(&input)
					if input["resource_id"] != "jira-a" {
						t.Error("checked a resource unrelated to next step")
					}
					json.NewEncoder(w).Encode(Check{ResourceID: "jira-a", Capability: "browse_issues", State: tc.permission, RequestID: "request-a"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			state, err := Advance(context.Background(), Client{BaseURL: server.URL}, "user-token", State{OwnerID: "user-a"}, Profile{ID: "user-a", CurrentTeamID: "team-a", Teams: []Team{{ID: "team-a"}}})
			if err != nil {
				t.Fatal(err)
			}
			if state.Status != tc.status || state.Widget.Type != tc.widget || checks != 1 {
				t.Fatalf("unexpected state: %+v checks=%d", state, checks)
			}
			if state.Status == "completed" {
				t.Fatal("access check cannot complete the whole goal")
			}
		})
	}
}
func TestUnavailableServiceDoesNotInventRights(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret details", 503) }))
	defer server.Close()
	_, err := Advance(context.Background(), Client{BaseURL: server.URL}, "token", State{OwnerID: "u"}, Profile{ID: "u", Teams: []Team{{ID: "t"}}})
	if err == nil {
		t.Fatal("unavailable dependency must fail")
	}
	if err.Error() != "team access service returned HTTP 503" {
		t.Fatal("remote response body leaked")
	}
}
func TestForeignOwnerRejectedBeforeRequest(t *testing.T) {
	_, err := Advance(context.Background(), Client{}, "token", State{OwnerID: "alice"}, Profile{ID: "bob"})
	if err == nil {
		t.Fatal("foreign owner accepted")
	}
}
func TestRequestUsesConfirmationAndIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "stable-key" || r.Header.Get("Authorization") != "Bearer user" {
			t.Error("missing identity/idempotency")
		}
		var input AccessInput
		json.NewDecoder(r.Body).Decode(&input)
		if !input.Confirmed || input.ResourceID != "jira-a" {
			t.Error("wrong action")
		}
		json.NewEncoder(w).Encode(Request{ID: "req", Status: "pending"})
	}))
	defer server.Close()
	v, err := (Client{BaseURL: server.URL}).CreateRequest(context.Background(), "user", "stable-key", AccessInput{ResourceID: "jira-a", Confirmed: true})
	if err != nil || v.ID != "req" {
		t.Fatalf("%+v %v", v, err)
	}
}
