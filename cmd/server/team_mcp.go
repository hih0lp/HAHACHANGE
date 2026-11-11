package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type teamMCPEmptyArg struct{}
type teamMCPTeamArg struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"team ID from team_profile; omit for the current team"`
}
type teamMCPAccessArg struct {
	ResourceID string `json:"resource_id" jsonschema:"resource ID from team_resources"`
	Capability string `json:"capability" jsonschema:"capability to check"`
}

// newTeamMCP exposes the team-access catalog as MCP tools. The bearer token is
// accepted by the gateway, used only for the downstream call, and never reaches
// the model or MCP result payload.
func newTeamMCP(endpoint string) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == r.Header.Get("Authorization") {
			token = ""
		}
		api := scenario.Client{BaseURL: endpoint}
		server := mcp.NewServer(&mcp.Implementation{Name: "hackchange-team-access", Version: "1.0.0"}, nil)
		mcp.AddTool(server, &mcp.Tool{Name: "team_profile", Description: "Read the current team-access profile and its teams."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ teamMCPEmptyArg) (*mcp.CallToolResult, any, error) {
			if token == "" {
				return nil, nil, errors.New("team-access authorization is required")
			}
			result := teamToolResult(ctx, api, token, "team_profile", "{}")
			return nil, result, nil
		})
		mcp.AddTool(server, &mcp.Tool{Name: "team_members", Description: "Read people, roles, mentor and lead in an authorized team."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg teamMCPTeamArg) (*mcp.CallToolResult, any, error) {
			return nil, teamToolResult(ctx, api, token, "team_members", `{"team_id":`+strconv.Quote(arg.TeamID)+`}`), nil
		})
		mcp.AddTool(server, &mcp.Tool{Name: "team_resources", Description: "Read resources visible in an authorized team."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg teamMCPTeamArg) (*mcp.CallToolResult, any, error) {
			return nil, teamToolResult(ctx, api, token, "team_resources", `{"team_id":`+strconv.Quote(arg.TeamID)+`}`), nil
		})
		mcp.AddTool(server, &mcp.Tool{Name: "access_check", Description: "Read one current access state in the training prototype."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg teamMCPAccessArg) (*mcp.CallToolResult, any, error) {
			return nil, teamToolResult(ctx, api, token, "access_check", `{"resource_id":`+strconv.Quote(arg.ResourceID)+`,"capability":`+strconv.Quote(arg.Capability)+`}`), nil
		})
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
