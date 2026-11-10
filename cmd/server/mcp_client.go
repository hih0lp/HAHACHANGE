package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var jiraMention = regexp.MustCompile(`(?i)\b[A-Z][A-Z0-9_]*-[0-9]+\b`)

type headerTransport struct {
	key  string
	base http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	if t.key != "" {
		copy.Header.Set("X-API-Key", t.key)
	}
	return t.base.RoundTrip(copy)
}

func mcpJiraContext(ctx context.Context, endpoint, key, question string) (string, bool, error) {
	issueKey := jiraMention.FindString(question)
	lower := strings.ToLower(question)
	mentionsJira := strings.Contains(lower, "jira") || strings.Contains(lower, "джир")
	wantsList := issueKey == "" && mentionsJira && (strings.Contains(lower, "задач") || strings.Contains(lower, "статус") || strings.Contains(lower, "тикет") || strings.Contains(lower, "issue"))
	if issueKey == "" && !wantsList {
		return "", false, nil
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "hackchange-agent", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Timeout: 20 * time.Second, Transport: headerTransport{key: key, base: http.DefaultTransport}}}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return "", true, err
	}
	defer session.Close()
	params := &mcp.CallToolParams{Name: "jira_list_issues", Arguments: map[string]any{}}
	if issueKey != "" {
		params.Name = "jira_get_issue"
		params.Arguments = map[string]string{"key": issueKey}
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		return "", true, err
	}
	if result.IsError {
		return "", true, errors.New("Jira MCP tool returned an error")
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return "", true, err
	}
	return string(data), true, nil
}
