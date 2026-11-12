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

func TestJiraIssueIncludesDates(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fields") != "summary,status,description,duedate,customfield_10015" {
			t.Errorf("fields=%s", r.URL.Query().Get("fields"))
		}
		_, _ = w.Write([]byte(`{"key":"SCRUM-7","fields":{"summary":"Task","status":{"name":"К выполнению"},"duedate":"2026-11-13","customfield_10015":"2026-11-10"}}`))
	}))
	defer jira.Close()
	issue, err := jiraGetIssue(context.Background(), jiraConfig{BaseURL: jira.URL, ProjectKey: "SCRUM"}, "SCRUM-7")
	if err != nil || issue.DueDate != "2026-11-13" || issue.StartDate != "2026-11-10" {
		t.Fatalf("issue=%+v err=%v", issue, err)
	}
}
