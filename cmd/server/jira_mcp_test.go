package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/rag"
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

func TestAskAddsJiraLinksInsideMessage(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/SCRUM-5":
			_, _ = w.Write([]byte(`{"key":"SCRUM-5","fields":{"summary":"Тестовая задача","status":{"name":"To Do"}}}`))
		case "/rest/api/3/search/jql":
			_, _ = w.Write([]byte(`{"issues":[{"key":"SCRUM-5","fields":{"summary":"Тестовая задача","status":{"name":"To Do"}}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer jira.Close()
	mcp := httptest.NewServer(newJiraMCP(jiraConfig{Mode: "live", BaseURL: jira.URL, ProjectKey: "SCRUM", Email: "test@example.org", APIToken: "token"}))
	defer mcp.Close()
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Это тестовая задача."}}]}`))
	}))
	defer llm.Close()
	t.Setenv("MCP_URL", mcp.URL)
	t.Setenv("LLM_URL", llm.URL)
	handler := testHandlerWithConfig(t, config{Jira: jiraConfig{Mode: "live"}, RAG: rag.Config{CollectionName: "playbook"}})
	for _, query := range []string{"Какой статус SCRUM-5?", "Покажи задачи Jira"} {
		response := call(handler, http.MethodPost, "/v1/ask", `{"query":"`+query+`"}`, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "SCRUM-5") || !strings.Contains(response.Body.String(), jira.URL+"/browse/SCRUM-5") {
			t.Fatalf("query %q: %d %s", query, response.Code, response.Body.String())
		}
	}
}
