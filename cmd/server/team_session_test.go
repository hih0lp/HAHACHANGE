package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func TestTeamSessionUsesServerConfiguredRoleCredentials(t *testing.T) {
	t.Setenv("TEAM_ACCESS_INTERN_PASSWORD", "intern-secret")
	t.Setenv("TEAM_ACCESS_INTERN2_PASSWORD", "second-secret")
	t.Setenv("TEAM_ACCESS_APPROVER_PASSWORD", "mentor-secret")
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input["password"] != map[string]string{"intern": "intern-secret", "intern2": "second-secret", "approver": "mentor-secret"}[input["username"]] {
			t.Errorf("wrong credentials for %q", input["username"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"signed","token_type":"Bearer","expires_in":900}`))
	}))
	defer service.Close()
	mux := http.NewServeMux()
	registerTeamSessionRoute(mux, scenario.Client{BaseURL: service.URL})
	for _, tc := range []struct{ role, expected string }{{"", "intern"}, {"intern2", "intern2"}, {"mentor", "mentor"}} {
		req := httptest.NewRequest(http.MethodPost, "/v1/team-access/session", nil)
		if tc.role != "" {
			req.AddCookie(&http.Cookie{Name: "buddy_demo_role", Value: tc.role})
		}
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK || resp.Header().Get("Cache-Control") != "no-store" || !strings.Contains(resp.Body.String(), `"role":"`+tc.expected+`"`) {
			t.Fatalf("%s: %d %s", tc.expected, resp.Code, resp.Body.String())
		}
	}
	t.Setenv("TEAM_ACCESS_INTERN2_PASSWORD", "")
	req := httptest.NewRequest(http.MethodPost, "/v1/team-access/session", nil)
	req.AddCookie(&http.Cookie{Name: "buddy_demo_role", Value: "intern2"})
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing credentials: %d", resp.Code)
	}
}
