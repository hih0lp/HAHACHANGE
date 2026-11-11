package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type reminderArgs struct {
	Project string `json:"project"`
}

func TestGitLabReminderRouteForwardsOnlyAuthorizedTeamSession(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_review_reminders"}, func(_ context.Context, _ *mcp.CallToolRequest, in reminderArgs) (*mcp.CallToolResult, map[string]any, error) {
		if in.Project != "2" {
			t.Errorf("wrong project %q", in.Project)
		}
		return nil, map[string]any{"reminders": []map[string]any{{"id": "gitlab:2:1", "iid": 1}}}, nil
	})
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer allowed" {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		stream.ServeHTTP(w, r)
	}))
	defer provider.Close()
	mux := http.NewServeMux()
	registerGitLabReminderRoute(mux, provider.URL, "2")
	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/gitlab/reminders", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/gitlab/reminders", nil)
	req.Header.Set("Authorization", "Bearer allowed")
	reply := httptest.NewRecorder()
	mux.ServeHTTP(reply, req)
	var result struct {
		Reminders []struct {
			ID string `json:"id"`
		} `json:"reminders"`
	}
	if reply.Code != http.StatusOK || json.Unmarshal(reply.Body.Bytes(), &result) != nil || len(result.Reminders) != 1 || result.Reminders[0].ID != "gitlab:2:1" {
		t.Fatalf("unexpected reminder response: %d %s", reply.Code, reply.Body.String())
	}
}
