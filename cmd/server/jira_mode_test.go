package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJiraDisabledByDefault(t *testing.T) {
	t.Setenv("JIRA_MODE", "")
	cfg := loadConfig().Jira
	if cfg.Mode != "off" {
		t.Fatalf("default Jira mode = %q, want off", cfg.Mode)
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("off mode should not require credentials: %v", err)
	}
	mux := http.NewServeMux()
	registerAccessRoutes(mux, "", cfg)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/access-requests", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled Jira action returned %d, want 404", response.Code)
	}
}

func TestJiraMockRequiresExplicitSelection(t *testing.T) {
	t.Setenv("JIRA_MODE", "mock")
	cfg := loadConfig().Jira
	if cfg.Mode != "mock" {
		t.Fatalf("explicit Jira mode = %q, want mock", cfg.Mode)
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
}
