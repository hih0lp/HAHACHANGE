package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type signedTransport struct{ token string }

func (tr signedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+tr.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestTeamToolsUseVerifiedProfileAndMembership(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer signed" {
			http.Error(w, "forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			w.Write([]byte(`{"id":"intern","current_team_id":"team-a"}`))
		case "/v1/teams/team-a/members":
			w.Write([]byte(`{"members":[{"id":"mentor","display_name":"Mentor"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()
	api := scenario.Client{BaseURL: service.URL}
	for _, tc := range []struct{ token, name, args, contains string }{
		{"", "team_profile", "", "нет авторизации"},
		{"signed", "team_profile", "{", "Некорректные"},
		{"signed", "team_profile", "", "intern"},
		{"signed", "team_members", "", "Mentor"},
		{"signed", "team_members", `{"team_id":"team-a"}`, "Mentor"},
		{"signed", "unknown", "", "Неизвестный"},
		{"denied", "team_members", "", "Не удалось определить"},
	} {
		result := teamToolResult(context.Background(), api, tc.token, tc.name, tc.args)
		encoded, _ := json.Marshal(result)
		if !strings.Contains(string(encoded), tc.contains) {
			t.Fatalf("%s: %s", tc.name, encoded)
		}
	}
}

func TestRightsMCPDelegatesJWTForReadAndConfirmedSubmit(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer signed" {
			http.Error(w, "forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/teams/team-a/resources":
			w.Write([]byte(`{"resources":[{"id":"jira","name":"Jira"}]}`))
		case "/v1/access/check":
			w.Write([]byte(`{"resource_id":"jira","capability":"browse_issues","state":"granted"}`))
		case "/v1/forms":
			w.Write([]byte(`{"forms":[{"id":"access-request","title":"Access","version":1,"kind":"access"}]}`))
		case "/v1/forms/access-request":
			w.Write([]byte(`{"id":"access-request","title":"Access","version":1,"kind":"access"}`))
		case "/v1/forms/access-request/validate":
			w.Write([]byte(`{"valid":true}`))
		case "/v1/forms/access-request/submit":
			if r.Header.Get("Idempotency-Key") != "stable-key" {
				t.Error("missing idempotency key")
			}
			w.Write([]byte(`{"id":"submitted"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()
	t.Setenv("RIGHTS_SELF_URL", service.URL)
	server := httptest.NewServer(newRightsMCP(true))
	defer server.Close()
	connect := func(token string) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "rights-test", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &http.Client{Transport: signedTransport{token: token}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	session := connect("signed")
	defer session.Close()
	for _, tc := range []struct {
		name     string
		args     map[string]any
		contains string
	}{
		{"rights_resources", map[string]any{"team_id": "team-a"}, "jira"},
		{"rights_access_check", map[string]any{"resource_id": "jira", "capability": "browse_issues"}, "granted"},
		{"rights_forms_list", map[string]any{}, "/forms/access-request"},
		{"rights_form_get", map[string]any{"form_id": "access-request"}, "Access"},
		{"rights_form_validate", map[string]any{"form_id": "access-request", "values": map[string]any{}}, "valid"},
		{"rights_form_submit", map[string]any{"form_id": "access-request", "version": 1, "values": map[string]any{}, "idempotency_key": "stable-key"}, "submitted"},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %+v %v", tc.name, result, err)
		}
		encoded, _ := json.Marshal(result.StructuredContent)
		if !strings.Contains(string(encoded), tc.contains) {
			t.Fatalf("%s: %s", tc.name, encoded)
		}
	}
	anonymous := connect("")
	defer anonymous.Close()
	result, err := anonymous.CallTool(context.Background(), &mcp.CallToolParams{Name: "rights_resources", Arguments: map[string]any{"team_id": "team-a"}})
	if err == nil && !result.IsError {
		t.Fatal("anonymous rights request succeeded")
	}
}
