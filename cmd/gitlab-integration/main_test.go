package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGitLabToolsRespectSubjectAndReadCommitFile(t *testing.T) {
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me":
			if r.Header.Get("Authorization") == "Bearer allowed" {
				fmt.Fprint(w, `{"id":"intern"}`)
			} else {
				fmt.Fprint(w, `{"id":"other"}`)
			}
		default:
			calls++
			if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v4/projects/platform%2Fteams/repository/files/internal%2Fteam.go/raw" || r.URL.Query().Get("ref") != "abc123" || r.Header.Get("PRIVATE-TOKEN") != "fixture-only" || r.Header.Get("Authorization") != "" {
				t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, "package team")
		}
	}))
	defer provider.Close()
	app := httptest.NewServer(bridge{gitlab: provider.URL, directory: provider.URL, subject: "intern", token: "fixture-only", client: provider.Client()}.handler())
	defer app.Close()

	ctx := context.Background()
	connect := func(bearer string) (*mcp.ClientSession, error) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		return client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: app.URL, HTTPClient: &http.Client{Transport: bearerTransport{bearer}}}, nil)
	}
	if denied, err := connect("denied"); err == nil {
		denied.Close()
		t.Fatal("unbound subject connected")
	}
	session, err := connect("allowed")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != len(operations) {
		t.Fatalf("tools/list: %v", err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "gitlab_file", Arguments: map[string]any{"project": "platform/teams", "ref": "abc123", "path": "internal/team.go"}})
	if err != nil || result.IsError {
		t.Fatalf("tools/call: %v", err)
	}
	data, ok := result.StructuredContent.(map[string]any)
	if !ok || data["content"] != "package team" || calls != 1 {
		t.Fatalf("unexpected GitLab result: %#v", result.StructuredContent)
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(copy)
}
