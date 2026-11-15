package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAccessAnswerUsesCurrentTeamAndVerifiedRights(t *testing.T) {
	teamServer := mcp.NewServer(&mcp.Implementation{Name: "team-test", Version: "1"}, nil)
	mcp.AddTool(teamServer, &mcp.Tool{Name: "team_profile"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"current_team_id": "team-a"}, nil
	})
	teamHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return teamServer }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer teamHTTP.Close()
	rightsServer := mcp.NewServer(&mcp.Implementation{Name: "rights-test", Version: "1"}, nil)
	mcp.AddTool(rightsServer, &mcp.Tool{Name: "rights_resources"}, func(_ context.Context, _ *mcp.CallToolRequest, args struct {
		TeamID string `json:"team_id"`
	}) (*mcp.CallToolResult, any, error) { if args.TeamID != "team-a" {
		t.Errorf("wrong team: %q", args.TeamID)
	}; return nil, []resourceChoice{{ID: "jira", Name: "Jira", Capabilities: []string{"browse", "edit"}}}, nil })
	mcp.AddTool(rightsServer, &mcp.Tool{Name: "rights_access_check"}, func(_ context.Context, _ *mcp.CallToolRequest, args struct {
		ResourceID string `json:"resource_id"`
		Capability string `json:"capability"`
	}) (*mcp.CallToolResult, any, error) {
		state := "missing"
		if args.Capability == "browse" {
			state = "granted"
		}
		return nil, map[string]string{"state": state}, nil
	})
	rightsHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return rightsServer }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer rightsHTTP.Close()
	a := answerer{teamMCPURL: teamHTTP.URL, rightsMCPURL: rightsHTTP.URL}
	resources, err := a.accessResources(context.Background(), "signed")
	if err != nil || len(resources) != 1 || resources[0].ID != "jira" {
		t.Fatalf("resources=%+v err=%v", resources, err)
	}
	for _, tc := range []struct {
		plan calendarPlan
		want string
	}{
		{calendarPlan{ResourceID: "jira"}, "browse: право выдано"},
		{calendarPlan{ResourceID: "jira", Capability: "edit"}, "в прототипе право не выдано"},
		{calendarPlan{ResourceID: "jira", Capability: "unknown"}, "не найдено в каталоге"},
		{calendarPlan{ResourceID: "missing"}, "Уточните его название"},
	} {
		if got := a.plannedAccessAnswer(context.Background(), "signed", tc.plan, resources); !strings.Contains(got, tc.want) {
			t.Fatalf("%+v: %s", tc.plan, got)
		}
	}
	if _, err := (answerer{}).accessResources(context.Background(), "signed"); err == nil {
		t.Fatal("unconfigured access services accepted")
	}
}
