package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerTransport struct {
	key    string
	bearer string
	base   http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	if t.key != "" {
		copy.Header.Set("X-API-Key", t.key)
	}
	if t.bearer != "" {
		copy.Header.Set("Authorization", "Bearer "+t.bearer)
	}
	return t.base.RoundTrip(copy)
}

func mcpTeamTool(ctx context.Context, endpoint, key, token, name string, arguments map[string]any) (any, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "ai-buddy-team-agent", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Timeout: 20 * time.Second, Transport: headerTransport{key: key, bearer: token, base: http.DefaultTransport}}}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	if result.IsError {
		return nil, errors.New("team-access MCP tool returned an error")
	}
	if result.StructuredContent == nil {
		return nil, fmt.Errorf("MCP tool %s returned no structured content", name)
	}
	log.Printf("MCP tools/call succeeded: tool=%s", name)
	return result.StructuredContent, nil
}

// mcpTeamTools obtains the tool catalogue from the actual MCP server. The
// agent never maintains a second, hand-written copy of that catalogue.
func mcpTeamTools(ctx context.Context, endpoint, key, token string) ([]map[string]any, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "ai-buddy-team-agent", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Timeout: 20 * time.Second, Transport: headerTransport{key: key, bearer: token, base: http.DefaultTransport}}}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	var result []map[string]any
	params := &mcp.ListToolsParams{}
	for {
		page, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, tool := range page.Tools {
			parameters, err := openAIToolParameters(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("MCP tool %q input schema: %w", tool.Name, err)
			}
			result = append(result, map[string]any{"type": "function", "function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": parameters,
			}})
		}
		if page.NextCursor == "" {
			return result, nil
		}
		params.Cursor = page.NextCursor
	}
}

// openAIToolParameters makes a generic MCP JSON schema acceptable to the
// OpenAI-compatible endpoint used by the local model. In particular, that
// endpoint requires properties even for an empty object. This does not add or
// rename tools; it only normalizes their schema representation.
func openAIToolParameters(input any) (map[string]any, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	parameters := map[string]any{}
	if len(encoded) != 0 && string(encoded) != "null" {
		if err := json.Unmarshal(encoded, &parameters); err != nil {
			return nil, err
		}
	}
	if parameters["type"] == nil {
		parameters["type"] = "object"
	}
	if parameters["type"] == "object" && parameters["properties"] == nil {
		parameters["properties"] = map[string]any{}
	}
	return parameters, nil
}
