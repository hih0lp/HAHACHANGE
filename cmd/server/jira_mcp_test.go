package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJiraCatalogueAndCallThroughMCP(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"issues":[{"key":"SCRUM-5","fields":{"summary":"Тестовая задача","status":{"name":"To Do"}}}]}`))
	}))
	defer jira.Close()
	server := httptest.NewServer(newJiraMCP(jiraConfig{Mode: "live", BaseURL: jira.URL, ProjectKey: "SCRUM", Email: "test@example.org", APIToken: "token"}))
	defer server.Close()
	tools, err := mcpTeamTools(context.Background(), server.URL, "", "")
	if err != nil || len(tools) != 2 {
		t.Fatalf("MCP catalogue: count=%d err=%v", len(tools), err)
	}
	result, err := mcpTeamTool(context.Background(), server.URL, "", "", "jira_list_issues", map[string]any{})
	if err != nil || result == nil {
		t.Fatalf("MCP tool call: result=%v err=%v", result, err)
	}
}
