package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type gitLabTestArg struct {
	Project string `json:"project"`
	Ref     string `json:"ref,omitempty"`
	ID      string `json:"id,omitempty"`
}

func TestGitLabReadAnswerUsesVerifiedProjectAndMRData(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "gitlab-test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_projects"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"data": []gitlabProject{{ID: 7, Name: "Demo", Path: "team/demo"}}}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_commits"}, func(_ context.Context, _ *mcp.CallToolRequest, arg gitLabTestArg) (*mcp.CallToolResult, any, error) {
		if arg.Project != "7" {
			t.Errorf("unverified project ID: %q", arg.Project)
		}
		return nil, map[string]any{"data": []gitlabCommit{{ID: "sha-1", Title: "First", Author: "Intern", Created: "2026-11-15T10:00:00Z"}}}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_commit_diff"}, func(_ context.Context, _ *mcp.CallToolRequest, arg gitLabTestArg) (*mcp.CallToolResult, any, error) {
		if arg.ID != "sha-1" {
			t.Errorf("wrong commit: %q", arg.ID)
		}
		return nil, map[string]any{"data": []map[string]string{{"old_path": "old.go", "new_path": "new.go", "diff": "--- a/old.go\n+++ b/new.go\n-old\n+new"}}}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_merge_requests"}, func(context.Context, *mcp.CallToolRequest, gitLabTestArg) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"data": []gitlabMR{{IID: 3, Title: "Review me"}}}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_approval_state"}, func(_ context.Context, _ *mcp.CallToolRequest, arg gitLabTestArg) (*mcp.CallToolResult, any, error) {
		if arg.ID != "3" {
			t.Errorf("wrong MR: %q", arg.ID)
		}
		return nil, map[string]any{"data": map[string]any{"rules": []map[string]any{{"name": "Code review", "approvals_required": 1, "approved": true}}}}, nil
	})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer httpServer.Close()
	a := answerer{gitLabMCPURL: httpServer.URL}
	for _, tc := range []struct {
		plan calendarPlan
		want string
	}{
		{calendarPlan{GitLabOperation: "commits"}, "sha-1"},
		{calendarPlan{GitLabOperation: "commit_diff", GitLabCommit: "sha-1"}, "old.go → new.go"},
		{calendarPlan{GitLabOperation: "merge_requests"}, "Review me"},
		{calendarPlan{GitLabOperation: "approval", GitLabMR: "!3"}, "одобрено"},
		{calendarPlan{GitLabOperation: "approval"}, "одобрено"},
		{calendarPlan{GitLabOperation: "unknown"}, "Не удалось определить"},
		{calendarPlan{GitLabOperation: "commits", GitLabProject: "other/project"}, "Укажите проект"},
	} {
		answer := a.gitLabReadAnswer(context.Background(), "signed", tc.plan)
		if !strings.Contains(answer, tc.want) {
			t.Fatalf("%+v: %s", tc.plan, answer)
		}
	}
}
