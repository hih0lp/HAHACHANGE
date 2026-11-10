package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAskRejectsInvalidLLMResponses(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"upstream failure", `{"error":"unavailable"}`, http.StatusServiceUnavailable},
		{"invalid JSON", `{`, http.StatusOK},
		{"missing choice", `{"choices":[]}`, http.StatusOK},
		{"blank answer", `{"choices":[{"message":{"content":"  "}}]}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer llm.Close()
			t.Setenv("LLM_URL", llm.URL)
			response := call(testHandler(t, ""), http.MethodPost, "/v1/ask", `{"query":"Как проходит ревью?"}`, "")
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAskReportsLLMConnectionFailure(t *testing.T) {
	t.Setenv("LLM_URL", "http://127.0.0.1:1")
	response := call(testHandler(t, ""), http.MethodPost, "/v1/ask", `{"query":"Как проходит ревью?"}`, "")
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestJiraConfigValidation(t *testing.T) {
	for _, cfg := range []jiraConfig{
		{Mode: "unexpected"},
		{Mode: "live", BaseURL: "http://example.org"},
		{Mode: "live", BaseURL: "https://example.org"},
	} {
		if err := cfg.validate(); err == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	if err := (jiraConfig{Mode: "mock"}).validate(); err != nil {
		t.Fatal(err)
	}
}

func TestJiraCreateIssueFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP error", http.StatusServiceUnavailable, `{"error":"down"}`},
		{"invalid response", http.StatusCreated, `{`},
		{"missing key", http.StatusCreated, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			tracker := jiraTracker{config: jiraConfig{BaseURL: server.URL, ProjectKey: "SCRUM", IssueType: "Task", Email: "test@example.org", APIToken: "token"}, client: server.Client()}
			if _, err := tracker.CreateIssue(context.Background(), issueInput{Resource: "Wiki", Reason: "Test"}); err == nil {
				t.Fatal("expected Jira error")
			}
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"SCRUM-9"}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	tracker := jiraTracker{config: jiraConfig{BaseURL: server.URL, ProjectKey: "SCRUM", IssueType: "Task", Email: "test@example.org", APIToken: "token"}, client: server.Client()}
	result, err := tracker.CreateIssue(context.Background(), issueInput{Resource: "Wiki", Reason: "Test"})
	if err != nil || result.Key != "SCRUM-9" || result.Status != "Created" {
		t.Fatalf("created issue without status lookup: %+v, %v", result, err)
	}
}

func TestAccessStoreRejectsCorruptOrUnwritableData(t *testing.T) {
	root := t.TempDir()
	badJSON := filepath.Join(root, "bad.json")
	if err := os.WriteFile(badJSON, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAccessStore(badJSON); err == nil {
		t.Fatal("corrupt store accepted")
	}
	mux := http.NewServeMux()
	registerAccessRoutes(mux, "", jiraConfig{Mode: "mock", ProjectKey: "SCRUM", DataPath: badJSON})
	if response := call(mux, http.MethodGet, "/v1/access-requests", "", ""); response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt store route = %d", response.Code)
	}
	store := &accessStore{path: filepath.Join(root, "missing", "requests.json"), records: []accessRequest{}}
	if err := os.WriteFile(filepath.Join(root, "missing"), []byte("block directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.save(accessRequest{ID: "one"}); err == nil || len(store.records) != 0 {
		t.Fatalf("failed save not rolled back: %v %+v", err, store.records)
	}
	if _, err := loadAccessStore(root); err == nil {
		t.Fatalf("directory read error = %v", err)
	}
}

func TestAccessRequestIdempotencyAndStorageFailure(t *testing.T) {
	store, err := loadAccessStore(filepath.Join(t.TempDir(), "requests.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerAccessRoutesWithStore(mux, "", jiraConfig{Mode: "mock"}, store, &stubTracker{})
	send := func(body, key string) int {
		r := httptest.NewRequest(http.MethodPost, "/v1/access-requests", strings.NewReader(body))
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	valid := `{"resource":"Wiki","reason":"Учебная задача","confirmed":true,"role":"intern"}`
	if got := send(valid, ""); got != http.StatusBadRequest {
		t.Fatalf("missing idempotency key = %d", got)
	}
	if got := send(valid, "stable-key"); got != http.StatusCreated {
		t.Fatalf("create = %d", got)
	}
	changed := `{"resource":"Jira","reason":"Учебная задача","confirmed":true,"role":"intern"}`
	if got := send(changed, "stable-key"); got != http.StatusConflict {
		t.Fatalf("reused key with different input = %d", got)
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(blocked, "requests.json")
	if got := send(changed, "new-key-123"); got != http.StatusInternalServerError || len(store.records) != 1 {
		t.Fatalf("store failure = %d, records = %d", got, len(store.records))
	}
}
