package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWiresGitLabMockAndProvider(t *testing.T) {
	stop := errors.New("listener closed")
	serve := func(server *http.Server) error {
		response := httptest.NewRecorder()
		path, expected := "/mcp", 403
		if os.Getenv("GITLAB_MODE") == "mock" {
			path, expected = "/healthz", 200
		}
		server.Handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != expected {
			t.Errorf("%s: status %d, want %d", path, response.Code, expected)
		}
		return stop
	}
	t.Setenv("GITLAB_MODE", "mock")
	t.Setenv("MOCK_DATABASE_URL", "")
	if err := run(serve); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	t.Setenv("GITLAB_MODE", "provider")
	t.Setenv("GITLAB_DEMO_TOKENS_FILE", "")
	t.Setenv("GITLAB_DEMO_TOKEN_FILE", "")
	if err := run(serve); err == nil || errors.Is(err, stop) {
		t.Fatalf("accepted missing token: %v", err)
	}
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITLAB_DEMO_TOKEN_FILE", path)
	t.Setenv("GITLAB_API_ORIGIN", "http://127.0.0.1:1/path")
	t.Setenv("TEAM_DIRECTORY_URL", "http://127.0.0.1:1")
	t.Setenv("GITLAB_DEMO_SUBJECT", "intern")
	if err := run(serve); err == nil || errors.Is(err, stop) {
		t.Fatalf("accepted invalid origin: %v", err)
	}
	t.Setenv("GITLAB_API_ORIGIN", "http://127.0.0.1:1")
	if err := run(serve); !errors.Is(err, stop) {
		t.Fatal(err)
	}
}
