package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testMCPServer(t *testing.T, server *mcp.Server) *httptest.Server {
	t.Helper()
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return httptest.NewServer(stream)
}

func TestMixedJiraCalendarAndDisconnectedGitLabKeepEveryAnswer(t *testing.T) {
	jira := mcp.NewServer(&mcp.Implementation{Name: "test-jira", Version: "1"}, nil)
	mcp.AddTool(jira, &mcp.Tool{Name: "jira_get_issue"}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		Key string `json:"key"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		if in.Key != "TASK-7" {
			t.Errorf("unexpected issue key %q", in.Key)
		}
		return nil, map[string]any{"key": in.Key, "summary": "Проверенная задача", "status": "В работе", "description": ""}, nil
	})
	jiraHTTP := testMCPServer(t, jira)
	defer jiraHTTP.Close()
	calendar := mcp.NewServer(&mcp.Implementation{Name: "test-calendar", Version: "1"}, nil)
	mcp.AddTool(calendar, &mcp.Tool{Name: "calendar_onboarding_events"}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct {
		TimeMin string `json:"time_min"`
		TimeMax string `json:"time_max"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"data": map[string]any{"items": []any{map[string]any{"summary": "Встреча", "start": map[string]string{"dateTime": "2099-11-19T13:00:00Z"}, "end": map[string]string{"dateTime": "2099-11-19T14:00:00Z"}}}}}, nil
	})
	calendarHTTP := testMCPServer(t, calendar)
	defer calendarHTTP.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"needs_jira_issue\":true,\"jira_issue_keys\":[\"TASK-7\"],\"needs_events\":true,\"needs_gitlab\":true,\"nearest_event_only\":true,\"time_min\":\"2099-11-19T00:00:00Z\",\"time_max\":\"2099-11-20T00:00:00Z\"}"}}]}`)
	}))
	defer model.Close()
	service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
	a := answerer{service: service, url: model.URL, model: "test", jiraMode: "mock", mcpURL: jiraHTTP.URL, calendarMCPURL: calendarHTTP.URL}
	answer, _, err := a.Ask(distributed.WithTeamToken(context.Background(), "session"), "Проверь задачу, встречу и GitLab")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"TASK-7", "Описание: не заполнено", "Ближайшее событие календаря", "Встреча", "GitLab сейчас отключён"} {
		if !strings.Contains(answer, expected) {
			t.Fatalf("missing %q in %q", expected, answer)
		}
	}
}

func TestAccessCheckUsesSelectedCatalogueResource(t *testing.T) {
	rights := mcp.NewServer(&mcp.Implementation{Name: "test-rights", Version: "1"}, nil)
	mcp.AddTool(rights, &mcp.Tool{Name: "rights_access_check"}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		ResourceID string `json:"resource_id"`
		Capability string `json:"capability"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		if in.ResourceID != "chosen-resource" || in.Capability != "read" {
			t.Errorf("unexpected check: %+v", in)
		}
		return nil, map[string]any{"state": "missing"}, nil
	})
	rightsHTTP := testMCPServer(t, rights)
	defer rightsHTTP.Close()
	a := answerer{rightsMCPURL: rightsHTTP.URL}
	answer := a.plannedAccessAnswer(context.Background(), "session", calendarPlan{ResourceID: "chosen-resource"}, []resourceChoice{{ID: "chosen-resource", Name: "Ресурс команды", Capabilities: []string{"read"}}})
	if !strings.Contains(answer, "Ресурс команды") || !strings.Contains(answer, "в прототипе право не выдано") || strings.Contains(answer, "chosen-resource") {
		t.Fatalf("wrong access answer: %q", answer)
	}
}

func TestGitLabOverviewUsesProjectsMCP(t *testing.T) {
	gitlab := mcp.NewServer(&mcp.Implementation{Name: "test-gitlab", Version: "1"}, nil)
	mcp.AddTool(gitlab, &mcp.Tool{Name: "gitlab_projects"}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"data": []any{map[string]string{"name": "Demo Project", "path_with_namespace": "demo/project"}}}, nil
	})
	mcpHTTP := testMCPServer(t, gitlab)
	defer mcpHTTP.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) < 2 ||
			!strings.Contains(request.Messages[0].Content, "The verb 'check' alone does not ask about permissions") {
			t.Error("model did not receive the GitLab intent distinction")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"needs_gitlab_projects\":true}"}}]}`)
	}))
	defer model.Close()
	service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
	a := answerer{service: service, url: model.URL, model: "test", jiraMode: "mock", mcpURL: mcpHTTP.URL, gitLabMCPURL: mcpHTTP.URL}
	answer, _, err := a.Ask(distributed.WithTeamToken(context.Background(), "session"), "проверь что у меня в гит лабе")
	if err != nil || !strings.Contains(answer, "Demo Project (demo/project)") || strings.Contains(answer, "проверки доступа") {
		t.Fatalf("wrong GitLab answer: %q, %v", answer, err)
	}
}

func TestMixedTeamAndPersonalJiraAvoidsInternalIDsAndUnverifiedDescription(t *testing.T) {
	team := mcp.NewServer(&mcp.Implementation{Name: "test-team", Version: "1"}, nil)
	mcp.AddTool(team, &mcp.Tool{Name: "team_profile"}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"jira_account_id": "account-private"}, nil
	})
	mcp.AddTool(team, &mcp.Tool{Name: "team_members"}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"members": []any{map[string]any{"id": "mentor-private", "display_name": "Наставник команды", "is_mentor": true, "is_lead": false}}}, nil
	})
	teamHTTP := testMCPServer(t, team)
	defer teamHTTP.Close()
	jira := mcp.NewServer(&mcp.Implementation{Name: "test-jira", Version: "1"}, nil)
	mcp.AddTool(jira, &mcp.Tool{Name: "jira_list_assigned_issues"}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		AccountID string `json:"account_id"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		if in.AccountID != "account-private" {
			t.Errorf("wrong account: %q", in.AccountID)
		}
		return nil, map[string]any{"project_key": "TASK", "issues": []any{map[string]any{"key": "TASK-7", "summary": "Проверенная задача", "status": "В работе"}}}, nil
	})
	jiraHTTP := testMCPServer(t, jira)
	defer jiraHTTP.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"needs_team_directory\":true,\"needs_my_jira_issues\":true}"}}]}`)
	}))
	defer model.Close()
	service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
	a := answerer{service: service, url: model.URL, model: "test", jiraMode: "mock", mcpURL: jiraHTTP.URL, teamMCPURL: teamHTTP.URL}
	answer, _, err := a.Ask(distributed.WithTeamToken(context.Background(), "session"), "Кто мой наставник и какие у меня задачи?")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Наставник команды", "TASK-7", "Проверенная задача"} {
		if !strings.Contains(answer, expected) {
			t.Fatalf("missing %q in %q", expected, answer)
		}
	}
	for _, forbidden := range []string{"mentor-private", "account-private", "Описание:"} {
		if strings.Contains(answer, forbidden) {
			t.Fatalf("unexpected %q in %q", forbidden, answer)
		}
	}
}
