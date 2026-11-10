package main

import (
	"context"
	"github.com/hackchange/avito-rag/internal/scenario"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyAccessAdapterStillPersists(t *testing.T) {
	mux := http.NewServeMux()
	registerAccessRoutes(mux, "", jiraConfig{Mode: "mock", ProjectKey: "TEST", DataPath: filepath.Join(t.TempDir(), "requests.json")})
	out := ownedRequest(mux, "POST", "/v1/access-requests", `{"resource":"repo","reason":"first task","role":"intern","confirmed":true}`, "", map[string]string{"Idempotency-Key": "stable-key"})
	if out.Code != 201 || !strings.Contains(out.Body.String(), "TEST-") {
		t.Fatal(out.Body.String())
	}
}
func TestJiraRejectsInvalidTransportConfiguration(t *testing.T) {
	cfg := jiraConfig{BaseURL: "://invalid", ProjectKey: "TEST"}
	if _, err := jiraGetIssue(context.Background(), cfg, "TEST-1"); err == nil {
		t.Fatal("invalid URL accepted")
	}
	if _, err := jiraListIssues(context.Background(), cfg); err == nil {
		t.Fatal("invalid URL accepted")
	}
	tracker := jiraTracker{config: cfg}
	if _, err := tracker.CreateIssue(context.Background(), issueInput{}); err == nil {
		t.Fatal("invalid URL accepted")
	}
	cfg.BaseURL = "http://127.0.0.1:1"
	if _, err := jiraGetIssue(context.Background(), cfg, "TEST-1"); err == nil {
		t.Fatal("network failure hidden")
	}
	if _, err := jiraGetIssue(context.Background(), cfg, "OTHER-1"); err == nil {
		t.Fatal("out of scope project accepted")
	}
	if adfText(map[string]any{"content": []any{map[string]any{"text": "description"}, "unknown"}}) != "description" {
		t.Fatal("ADF traversal failed")
	}
}
func TestScenarioLoginUpstreamFailure(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 503) }))
	defer service.Close()
	mux := http.NewServeMux()
	registerScenarioRoutes(mux, scenario.Store{}, scenario.Client{BaseURL: service.URL})
	if out := ownedRequest(mux, "POST", "/v1/scenarios/login", `{"username":"a","password":"b"}`, "", nil); out.Code != 503 {
		t.Fatal("upstream login failure hidden")
	}
}
