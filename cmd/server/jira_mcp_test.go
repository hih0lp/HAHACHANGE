package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if err != nil || len(tools) != 4 {
		t.Fatalf("MCP catalogue: count=%d err=%v", len(tools), err)
	}
	for _, tool := range tools {
		function := tool["function"].(map[string]any)
		if function["name"] == "jira_list_issues" && !strings.Contains(function["description"].(string), "not a personal task list") {
			t.Fatal("Jira list description does not explain its project scope")
		}
	}
	result, err := mcpTeamTool(context.Background(), server.URL, "", "", "jira_list_issues", map[string]any{})
	if err != nil || result == nil {
		t.Fatalf("MCP tool call: result=%v err=%v", result, err)
	}
}

func TestJiraExpandedFields(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fields") != "*all" || r.URL.Query().Get("expand") != "names" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"key":"SCRUM-7","names":{"customfield_10015":"Дата начала","duedate":"Срок исполнения"},"fields":{"customfield_10015":"2026-11-10","duedate":"2026-11-13","unused":null}}`))
	}))
	defer jira.Close()
	result, err := jiraGetAllFields(context.Background(), jiraConfig{BaseURL: jira.URL, ProjectKey: "SCRUM"}, "SCRUM-7")
	if err != nil {
		t.Fatal(err)
	}
	fields := result["fields"].(map[string]json.RawMessage)
	if string(fields["Дата начала (customfield_10015)"]) != `"2026-11-10"` || len(fields) != 2 {
		t.Fatalf("fields=%v", fields)
	}
}

func TestJiraIssueIncludesDates(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fields") != "summary,status,description,duedate,customfield_10015,assignee" {
			t.Errorf("fields=%s", r.URL.Query().Get("fields"))
		}
		_, _ = w.Write([]byte(`{"key":"SCRUM-7","fields":{"summary":"Task","status":{"name":"К выполнению","statusCategory":{"name":"Нужно сделать"}},"duedate":"2026-11-13","customfield_10015":"2026-11-10"}}`))
	}))
	defer jira.Close()
	issue, err := jiraGetIssue(context.Background(), jiraConfig{BaseURL: jira.URL, ProjectKey: "SCRUM"}, "SCRUM-7")
	if err != nil || issue.DueDate != "2026-11-13" || issue.StartDate != "2026-11-10" || issue.StatusCategory != "Нужно сделать" {
		t.Fatalf("issue=%+v err=%v", issue, err)
	}
}

func TestJiraAssignedSearchUsesAccountIDAndRejectsOtherAssignees(t *testing.T) {
	const accountID = "712020:dac8b504-c5af-48c6-961f-501520b7615d"
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("jql"), `assignee = "`+accountID+`"`) || r.URL.Query().Get("fields") != "summary,status,assignee,duedate" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"issues":[{"key":"SCRUM-7","fields":{"summary":"Assigned","assignee":{"accountId":"` + accountID + `"}}},{"key":"SCRUM-6","fields":{"summary":"Other","assignee":{"accountId":"someone-else"}}}]}`))
	}))
	defer jira.Close()
	issues, err := jiraSearchIssues(context.Background(), jiraConfig{BaseURL: jira.URL, ProjectKey: "SCRUM"}, accountID)
	if err != nil || len(issues.Issues) != 1 || issues.Issues[0].Key != "SCRUM-7" {
		t.Fatalf("issues=%+v err=%v", issues, err)
	}
	encoded, _ := json.Marshal(issues)
	if strings.Contains(string(encoded), "description") || issues.ProjectKey != "SCRUM" {
		t.Fatalf("list invents detail fields or omits scope: %s", encoded)
	}
	if _, err := jiraSearchIssues(context.Background(), jiraConfig{ProjectKey: "SCRUM"}, `x" OR project = OTHER`); err == nil {
		t.Fatal("unsafe Jira accountId accepted")
	}
}

func TestVerifiedJiraAccountID(t *testing.T) {
	profile := `{"source":"team-access","profile":{"id":"intern","jira_account_id":"account-123"}}`
	if got := verifiedJiraAccountID(profile); got != "account-123" {
		t.Fatalf("account=%q", got)
	}
	if got := verifiedJiraAccountID(`{"profile":{}}`); got != "" {
		t.Fatalf("missing account=%q", got)
	}
	if got := verifiedJiraAccountID(`{"id":"intern","jira_account_id":"account-456"}`); got != "account-456" {
		t.Fatalf("MCP account=%q", got)
	}
}
