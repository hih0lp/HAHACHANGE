package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type calendarBearerTransport struct{ token string }

func (tr calendarBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header.Set("Authorization", "Bearer "+tr.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestCalendarHTTPAndMCP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mentor.json")
	companyPath := filepath.Join(t.TempDir(), "company.json")
	mentor := `{"calendar_id":"mentor","summary":"Mentor","time_zone":"Europe/Moscow","events":[{"id":"meeting","summary":"1:1","start":{"dateTime":"2026-11-17T15:30:00+03:00"},"end":{"dateTime":"2026-11-17T16:00:00+03:00"}},{"id":"cancelled","summary":"Cancelled","status":"cancelled","start":{"dateTime":"2026-11-17T18:00:00+03:00"},"end":{"dateTime":"2026-11-17T19:00:00+03:00"}}]}`
	company := `{"calendar_id":"company","summary":"Events","time_zone":"Europe/Moscow","events":[{"id":"party","summary":"Party","start":{"dateTime":"2026-11-18T12:00:00+03:00"},"end":{"dateTime":"2026-11-18T13:00:00+03:00"}}]}`
	for file, content := range map[string]string{path: mentor, companyPath: company} {
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" || r.Header.Get("Authorization") != "Bearer allowed" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "intern-1"})
	}))
	defer directory.Close()
	calendar := httptest.NewServer(newMockCalendarHandler(path, companyPath, directory.URL, calendarSubjects(" intern-1, intern-2 "), nil, nil))
	defer calendar.Close()
	if got := calendarSubjects(" intern-1, intern-2 "); !got["intern-1"] || !got["intern-2"] || len(got) != 2 {
		t.Fatalf("invalid subject parsing: %v", got)
	}
	resp, err := http.Get(calendar.URL + "/health")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("health: %v %v", resp, err)
	}
	resp.Body.Close()
	request, _ := http.NewRequest(http.MethodPost, calendar.URL+"/mcp", nil)
	resp, err = http.DefaultClient.Do(request)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing token: %v %v", resp, err)
	}
	resp.Body.Close()
	request.Header.Set("Authorization", "Bearer denied")
	resp, err = http.DefaultClient.Do(request)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid token: %v %v", resp, err)
	}
	resp.Body.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "calendar-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: calendar.URL + "/mcp", HTTPClient: &http.Client{Transport: calendarBearerTransport{token: "allowed"}}}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	period := map[string]any{"time_min": "2026-11-17T00:00:00+03:00", "time_max": "2026-11-19T00:00:00+03:00"}
	for _, tc := range []struct{ tool, contains, excludes string }{
		{"calendar_onboarding_events", "1:1", "Cancelled"},
		{"calendar_mentor_freebusy", "calendar#freeBusy", "1:1"},
		{"calendar_company_events", "Party", "1:1"},
	} {
		result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.tool, Arguments: period})
		if callErr != nil || result.IsError {
			t.Fatalf("%s: %+v %v", tc.tool, result, callErr)
		}
		encoded, _ := json.Marshal(result.StructuredContent)
		if !strings.Contains(string(encoded), tc.contains) || strings.Contains(string(encoded), tc.excludes) {
			t.Fatalf("%s: %s", tc.tool, encoded)
		}
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "calendar_onboarding_events", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("unbounded onboarding: %+v %v", result, err)
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "calendar_mentor_freebusy", Arguments: map[string]any{"time_min": "bad"}})
	if err == nil && !result.IsError {
		t.Fatal("invalid period accepted")
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	resp, err = http.Get(calendar.URL + "/health")
	if err != nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("invalid fixture health: %v %v", resp, err)
	}
	resp.Body.Close()
}
