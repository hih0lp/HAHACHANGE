package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMockItemPutProxiesBearerAndPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/v1/mock-data/gitlab/projects/2/branches/docs/demo" || r.Header.Get("Authorization") != "Bearer intern" {
			t.Errorf("unexpected upstream request: %s %s %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":2}`))
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	registerMockEditors(mux, "", map[string]string{"gitlab": upstream.URL}, "")
	r := httptest.NewRequest("PUT", "/v1/mock-data/gitlab/projects/2/branches/docs/demo", strings.NewReader(`{"name":"docs/new"}`))
	r.Header.Set("Authorization", "Bearer intern")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":2`) {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
}

func TestGitLabBranchListUsesCallerTokenAndProject(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_branches"}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		Project string `json:"project"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		if in.Project != "2" {
			t.Errorf("project = %q", in.Project)
		}
		return nil, map[string]any{"data": []map[string]string{{"name": "docs/demo"}}}, nil
	})
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer intern" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		stream.ServeHTTP(w, r)
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	registerMockEditors(mux, "", map[string]string{}, upstream.URL)
	req := httptest.NewRequest("GET", "/v1/tasks/gitlab/2/branches", nil)
	req.Header.Set("Authorization", "Bearer intern")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var result struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Data) != 1 || result.Data[0].Name != "docs/demo" {
		t.Fatalf("branches: %d %s", w.Code, w.Body.String())
	}
}
