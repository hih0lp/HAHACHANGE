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

// newTeamMCP exposes the directory's own MCP tools. The caller's bearer token
// is forwarded to the directory HTTP API and never enters the model result.
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
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
