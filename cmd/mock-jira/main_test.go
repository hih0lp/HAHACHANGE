package main

import (
	"context"
	"encoding/json"
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
