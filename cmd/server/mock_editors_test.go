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

func TestMockJiraItemGetProxiesBearer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/mock-data/jira/issues/SCRUM-1" || r.Header.Get("Authorization") != "Bearer intern" {
			t.Errorf("unexpected upstream request: %s %s %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"SCRUM-1","description":"Saved details"}`))
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	registerMockEditors(mux, "", map[string]string{"jira": upstream.URL}, "")
	r := httptest.NewRequest("GET", "/v1/mock-data/jira/issues/SCRUM-1", nil)
	r.Header.Set("Authorization", "Bearer intern")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"description":"Saved details"`) {
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

func TestMockEditorRejectsMissingIdentityAndBadUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>wrong upstream</html>"))
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	registerMockEditors(mux, "api-key", map[string]string{"jira": upstream.URL, "invalid": "file:///tmp/data"}, "http://127.0.0.1:1/mcp")
	call := func(path, token, key string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", token)
		req.Header.Set("X-API-Key", key)
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		if resp.Code != want {
			t.Fatalf("%s: got %d want %d: %s", path, resp.Code, want, resp.Body.String())
		}
	}
	call("/v1/mock-data/invalid", "Bearer intern", "api-key", 404)
	call("/v1/mock-data/jira", "Bearer intern", "", 401)
	call("/v1/mock-data/jira", "", "api-key", 401)
	call("/v1/mock-data/jira", "Bearer intern", "api-key", 503)
	call("/v1/tasks/gitlab", "", "api-key", 401)
	call("/v1/tasks/gitlab", "Bearer intern", "api-key", 503)
	call("/v1/tasks/gitlab/2/branches", "", "api-key", 401)
	call("/v1/tasks/gitlab/2/branches", "Bearer intern", "api-key", 503)
}
