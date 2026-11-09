package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type stubTracker struct {
	calls   int
	failure error
}

func (s *stubTracker) CreateIssue(_ context.Context, _ issueInput) (issueResult, error) {
	s.calls++
	if s.failure != nil {
		return issueResult{}, s.failure
	}
	return issueResult{Key: "SCRUM-42", Status: "To Do", URL: "https://example.atlassian.net/browse/SCRUM-42"}, nil
}

func TestAccessRequestLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	cfg := jiraConfig{Mode: "mock", ProjectKey: "SCRUM", DataPath: path}
	store, err := loadAccessStore(path)
	if err != nil {
		t.Fatal(err)
	}
	tracker := &stubTracker{}
	mux := http.NewServeMux()
	registerAccessRoutesWithStore(mux, "secret", cfg, store, tracker)
	body := `{"resource":"тестовое пространство","reason":"учебный онбординг","confirmed":true,"role":"intern"}`
	send := func(idem string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/access-requests", strings.NewReader(body))
		r.Header.Set("X-API-Key", "secret")
		r.Header.Set("Idempotency-Key", idem)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if r := send("stable-key-123"); r.Code != 201 {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	if r := send("stable-key-123"); r.Code != 200 {
		t.Fatalf("duplicate: %d %s", r.Code, r.Body.String())
	}
	if tracker.calls != 1 {
		t.Fatalf("Jira called %d times", tracker.calls)
	}
	reopened, err := loadAccessStore(path)
	if err != nil || len(reopened.records) != 1 {
		t.Fatalf("persisted records: %v %+v", err, reopened)
	}
	id := reopened.records[0].ID
	if r := call(mux, "GET", "/v1/access-requests/"+id, "", "secret"); r.Code != 200 || !strings.Contains(r.Body.String(), "SCRUM-42") {
		t.Fatalf("get: %d %s", r.Code, r.Body.String())
	}
	if r := call(mux, "GET", "/v1/access-requests", "", "secret"); r.Code != 200 || !strings.Contains(r.Body.String(), "SCRUM-42") {
		t.Fatalf("list: %d %s", r.Code, r.Body.String())
	}
	if r := call(mux, "GET", "/v1/access-requests", "", ""); r.Code != 401 {
		t.Fatalf("auth: %d", r.Code)
	}
	if r := call(mux, "GET", "/v1/access-requests/missing", "", "secret"); r.Code != 404 {
		t.Fatalf("missing: %d", r.Code)
	}
}

func TestAccessRequestRejectsInvalidAndFailure(t *testing.T) {
	cfg := jiraConfig{Mode: "mock", ProjectKey: "SCRUM", DataPath: filepath.Join(t.TempDir(), "requests.json")}
	store, _ := loadAccessStore(cfg.DataPath)
	tracker := &stubTracker{failure: errors.New("Jira unavailable")}
	mux := http.NewServeMux()
	registerAccessRoutesWithStore(mux, "", cfg, store, tracker)
	cases := []struct {
		body string
		want int
	}{
		{`{`, 400},
		{`{"resource":"ok","reason":"ok","confirmed":false,"role":"intern"}`, 400},
		{`{"resource":"ok","reason":"ok","confirmed":true,"role":"admin"}`, 403},
		{`{"resource":"x","reason":"ok","confirmed":true,"role":"intern"}`, 400},
		{`{"resource":"ok","reason":"ok","confirmed":true,"role":"intern"}`, 502},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("POST", "/v1/access-requests", strings.NewReader(tc.body))
		r.Header.Set("Idempotency-Key", "key-123456")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s: got %d want %d", tc.body, w.Code, tc.want)
		}
	}
	if len(store.records) != 0 {
		t.Fatal("failed Jira request was persisted")
	}
}

func TestJiraCreateIssue(t *testing.T) {
	var sawCreate bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/rest/api/3/issue" {
			sawCreate = true
			user, pass, ok := r.BasicAuth()
			if !ok || user != "person@example.test" || pass != "token" {
				t.Error("bad auth")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			fields := body["fields"].(map[string]any)
			if fields["summary"] != "Hack&Change: тестовая заявка на доступ к Wiki" {
				t.Errorf("summary=%v", fields["summary"])
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"key":"SCRUM-42"}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/rest/api/3/issue/SCRUM-42" {
			w.Write([]byte(`{"fields":{"status":{"name":"To Do"}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	cfg := jiraConfig{Mode: "live", BaseURL: server.URL, ProjectKey: "SCRUM", IssueType: "Task", Email: "person@example.test", APIToken: "token"}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	tracker := jiraTracker{config: cfg, client: server.Client()}
	result, err := tracker.CreateIssue(context.Background(), issueInput{"Wiki", "Учебный онбординг"})
	if err != nil || !sawCreate || result.Key != "SCRUM-42" || result.Status != "To Do" || !strings.Contains(result.URL, "/browse/SCRUM-42") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
