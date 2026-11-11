package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type calendarReminderArgs struct {
	TimeMin string `json:"time_min"`
	TimeMax string `json:"time_max"`
}

func TestCalendarReminderRouteUsesBearerAndNextDay(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "calendar_onboarding_events"}, func(_ context.Context, _ *mcp.CallToolRequest, in calendarReminderArgs) (*mcp.CallToolResult, map[string]any, error) {
		start, e1 := time.Parse(time.RFC3339, in.TimeMin)
		end, e2 := time.Parse(time.RFC3339, in.TimeMax)
		if e1 != nil || e2 != nil || end.Sub(start) != 24*time.Hour {
			t.Errorf("wrong reminder window: %q %q", in.TimeMin, in.TimeMax)
		}
		return nil, map[string]any{"data": map[string]any{"items": []map[string]any{{"summary": "demo"}}}}, nil
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
	registerCalendarReminderRoute(mux, provider.URL)
	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/calendar/reminders", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/reminders", nil)
	req.Header.Set("Authorization", "Bearer allowed")
	reply := httptest.NewRecorder()
	mux.ServeHTTP(reply, req)
	var result struct {
		Data struct {
			Items []struct {
				Summary string `json:"summary"`
			} `json:"items"`
		} `json:"data"`
	}
	if reply.Code != http.StatusOK || json.Unmarshal(reply.Body.Bytes(), &result) != nil || len(result.Data.Items) != 1 {
		t.Fatalf("unexpected calendar response: %d %s", reply.Code, reply.Body.String())
	}
}
