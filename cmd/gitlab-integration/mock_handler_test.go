package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMockGitLabHandlerServesHealthAndAuthenticatedMCP(t *testing.T) {
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer intern" {
			http.Error(w, "forbidden", 403)
			return
		}
		fmt.Fprint(w, `{"id":"intern"}`)
	}))
	defer directory.Close()
	app := httptest.NewUnstartedServer(nil)
	app.Config.Handler = newMockGitLabHandler(nil, directory.URL, "http://"+app.Listener.Addr().String())
	app.Start()
	defer app.Close()
	resp, err := http.Get(app.URL + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("health: %v %v", resp, err)
	}
	resp.Body.Close()
	resp, err = http.Get(app.URL + "/api/v4/projects")
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("direct GitLab call: %v %v", resp, err)
	}
	resp.Body.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mock-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: app.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{"intern"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "gitlab_projects", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("projects: %+v err=%v", result, err)
	}
}
