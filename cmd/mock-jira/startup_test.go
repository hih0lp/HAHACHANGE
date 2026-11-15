package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunServesConfiguredFixture(t *testing.T) {
	t.Setenv("MOCK_DATABASE_URL", "")
	t.Setenv("MOCK_JIRA_JSON", "")
	t.Setenv("MOCK_LISTEN", "")

	stopped := errors.New("listener closed")
	serve := func(addr string, handler http.Handler) error {
		if addr != "127.0.0.1:8080" {
			t.Fatal(addr)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/health", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("health = %d: %s", response.Code, response.Body)
		}
		return stopped
	}
	if err := run(serve); err == nil || errors.Is(err, stopped) {
		t.Fatalf("missing configuration: %v", err)
	}
	t.Setenv("MOCK_JIRA_JSON", "../../fixtures/mock/jira.json")
	if err := run(serve); !errors.Is(err, stopped) {
		t.Fatal(err)
	}
}
