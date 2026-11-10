package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentReadsJiraThroughMCP(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user@example.test" || pass != "test-token" {
			t.Error("Jira credentials missing")
		}
		switch r.URL.Path {
		case "/rest/api/3/issue/SCRUM-5":
			w.Write([]byte(`{"key":"SCRUM-5","fields":{"summary":"Тестовая заявка","status":{"name":"К выполнению"},"description":{"content":[{"content":[{"type":"text","text":"Учебный запрос"}]}]}}}`))
		case "/rest/api/3/search/jql":
			w.Write([]byte(`{"issues":[{"key":"SCRUM-5","fields":{"summary":"Тестовая заявка","status":{"name":"К выполнению"}}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer jira.Close()
	cfg := jiraConfig{Mode: "live", BaseURL: jira.URL, ProjectKey: "SCRUM", Email: "user@example.test", APIToken: "test-token"}
	mcpServer := httptest.NewServer(newJiraMCP(cfg))
	defer mcpServer.Close()
	issue, used, err := mcpJiraContext(context.Background(), mcpServer.URL, "", "Какой статус SCRUM-5?")
	if err != nil || !used || !strings.Contains(issue, "Учебный запрос") || !strings.Contains(issue, "К выполнению") {
		t.Fatalf("issue: used=%v err=%v data=%s", used, err, issue)
	}
	list, used, err := mcpJiraContext(context.Background(), mcpServer.URL, "", "Покажи задачи Jira")
	if err != nil || !used || !strings.Contains(list, "SCRUM-5") {
		t.Fatalf("list: used=%v err=%v data=%s", used, err, list)
	}
	if _, used, err := mcpJiraContext(context.Background(), mcpServer.URL, "", "Как пройти онбординг?"); used || err != nil {
		t.Fatalf("unrelated query: used=%v err=%v", used, err)
	}
}
