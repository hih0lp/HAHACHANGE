package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestMockJiraScopesMCPToJWTAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jira.json")
	data := `{"project_key":"SCRUM","issues":[{"key":"SCRUM-1","summary":"First","status":"Open","assignee_account_id":"first"},{"key":"SCRUM-2","summary":"Second","status":"Open","assignee_account_id":"second"}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	team := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" || r.Header.Get("Authorization") != "Bearer intern2" {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "intern2", "jira_account_id": "second"})
	}))
	defer team.Close()
	jira := httptest.NewServer(newMockJiraHandler(path, scenario.Client{BaseURL: team.URL}))
	defer jira.Close()
	request, _ := http.NewRequest(http.MethodPost, jira.URL+"/mcp", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token response=%v error=%v", response, err)
	}
	response.Body.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "jira-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: jira.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "intern2"}}}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "jira_list_issues", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("list failed: %+v %v", result, err)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(encoded), "SCRUM-2") || strings.Contains(string(encoded), "SCRUM-1") {
		t.Fatalf("unscoped list: %s", encoded)
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "jira_get_issue", Arguments: map[string]any{"key": "SCRUM-1"}})
	if err == nil && !result.IsError {
		t.Fatal("other intern's issue was readable")
	}
}

func TestFixtureReloadAndPersonalList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jira.json")
	write := func(summary string) {
		t.Helper()
		b := []byte(`{"project_key":"SCRUM","issues":[{"key":"SCRUM-7","summary":"` + summary + `","status":"В работе","description":"","assignee_account_id":"person"},{"key":"SCRUM-6","summary":"Other","status":"К выполнению","description":""}]}`)
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("First")
	f, e := load(path)
	if e != nil {
		t.Fatal(e)
	}
	got := list(f, "person", 50)["issues"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["summary"] != "First" {
		t.Fatal(got)
	}
	write("Changed")
	f, e = load(path)
	if e != nil {
		t.Fatal(e)
	}
	v, e := find(f, "SCRUM-7")
	if e != nil || v.Summary != "Changed" || v.Description != "" {
		t.Fatalf("%+v %v", v, e)
	}
	if e = os.WriteFile(path, []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = load(path); e == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestFixtureRejectsInvalidDate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jira.json")
	data := []byte(`{"project_key":"SCRUM","issues":[{"key":"SCRUM-7","summary":"Task","status":"In progress","due_date":"2026-11-31"}]}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err == nil {
		t.Fatal("invalid date accepted")
	}
}

func TestFixtureAcceptsExactDeadlineAndRejectsInvalidTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jira.json")
	write := func(dueAt string) error {
		data := []byte(`{"project_key":"SCRUM","issues":[{"key":"SCRUM-8","summary":"Reminder test","status":"In progress","due_at":"` + dueAt + `"}]}`)
		return os.WriteFile(path, data, 0600)
	}
	if err := write("2026-11-14T22:15:00+03:00"); err != nil {
		t.Fatal(err)
	}
	fixture, err := load(path)
	if err != nil || fixture.Issues[0].DueAt != "2026-11-14T22:15:00+03:00" {
		t.Fatalf("fixture=%+v err=%v", fixture, err)
	}
	if err := write("2026-11-14 22:15"); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err == nil {
		t.Fatal("invalid exact deadline accepted")
	}
}

func TestMockJiraRESTAndMCPFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jira.json")
	data := `{"project_key":"SCRUM","issues":[{"key":"SCRUM-2","summary":"Second","status":"In progress","description":"Detailed work","due_date":"2026-11-19","due_at":"2026-11-19T15:00:00+03:00","start_date":"2026-11-18","assignee_account_id":"second","assignee_name":"Intern Two","fields":{"customfield_7":"value","empty":null}},{"key":"SCRUM-1","summary":"First","status":"Open","description":"First details","assignee_account_id":"first"}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	team := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer intern2" {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "intern2", "jira_account_id": "second"})
	}))
	defer team.Close()
	jira := httptest.NewServer(newMockJiraHandler(path, scenario.Client{BaseURL: team.URL}))
	defer jira.Close()
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/health", http.StatusNoContent, ""},
		{"/rest/api/3/issue/SCRUM-2", http.StatusOK, "Detailed work"},
		{"/rest/api/3/issue/SCRUM-2?expand=names", http.StatusOK, "Название"},
		{"/rest/api/3/issue/SCRUM-404", http.StatusNotFound, ""},
		{"/rest/api/3/search/jql?jql=assignee%20%3D%20%22second%22", http.StatusOK, "SCRUM-2"},
	} {
		resp, err := http.Get(jira.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status || !strings.Contains(string(body), tc.contains) {
			t.Fatalf("%s: status=%d body=%s", tc.path, resp.StatusCode, body)
		}
		if strings.Contains(tc.path, "search/jql") && strings.Contains(string(body), "SCRUM-1") {
			t.Fatalf("JQL leaked another intern's issue: %s", body)
		}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "jira-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: jira.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "intern2"}}}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, tc := range []struct {
		name      string
		args      map[string]any
		contains  string
		wantError bool
	}{
		{"jira_list_assigned_issues", map[string]any{"account_id": "second"}, "SCRUM-2", false},
		{"jira_list_assigned_issues", map[string]any{"account_id": "first"}, "", true},
		{"jira_get_issue", map[string]any{"key": "SCRUM-2"}, "Detailed work", false},
		{"jira_get_issue_fields", map[string]any{"key": "SCRUM-2"}, "customfield_7", false},
		{"jira_get_issue_fields", map[string]any{"key": "SCRUM-1"}, "", true},
		{"jira_get_issue", map[string]any{"key": "SCRUM-404"}, "", true},
	} {
		result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		failed := callErr != nil || result.IsError
		if failed != tc.wantError {
			t.Fatalf("%s(%v): result=%+v err=%v", tc.name, tc.args, result, callErr)
		}
		if !failed {
			encoded, _ := json.Marshal(result.StructuredContent)
			if !strings.Contains(string(encoded), tc.contains) || strings.Contains(string(encoded), "SCRUM-1") {
				t.Fatalf("%s returned unscoped/invalid data: %s", tc.name, encoded)
			}
		}
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(jira.URL + "/health")
	if err != nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("invalid fixture health: %v %v", resp, err)
	}
	resp.Body.Close()
}
