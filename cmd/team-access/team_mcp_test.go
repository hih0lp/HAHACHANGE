package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTeamMCPCatalog(t *testing.T) {
	server := httptest.NewServer(newTeamMCP("http://127.0.0.1:1"))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "team-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	page, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range page.Tools {
		found[tool.Name] = true
	}
	if len(found) != 2 || !found["team_profile"] || !found["team_members"] {
		t.Fatalf("unexpected team tools: %v", found)
	}
}
