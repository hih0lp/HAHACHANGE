package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestRunBindsTeamAndRights(t *testing.T) {
	stop := errors.New("listener closed")
	serve := func(server *http.Server) error {
		if server.Addr != ":8080" {
			t.Errorf("address = %s", server.Addr)
		}
		health := httptest.NewRecorder()
		server.Handler.ServeHTTP(health, httptest.NewRequest("GET", "/health", nil))
		if health.Code != 200 {
			t.Errorf("health = %d", health.Code)
		}
		mcp := httptest.NewRecorder()
		server.Handler.ServeHTTP(mcp, httptest.NewRequest("POST", "/mcp", strings.NewReader("{}")))
		if mcp.Code == 404 {
			t.Error("MCP route was not registered")
		}
		return stop
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("DATABASE_URL", "")
	if err := run(ctx, serve); err == nil || errors.Is(err, stop) {
		t.Fatalf("missing DSN: %v", err)
	}
	t.Setenv("DATABASE_URL", testsupport.IsolatedURL(testsupport.Postgres(t)))
	t.Setenv("TEAM_ACCESS_KEY_FILE", filepath.Join(t.TempDir(), "key.pem"))
	t.Setenv("TEAM_ACCESS_INTERN_PASSWORD", "test-intern-password")
	t.Setenv("TEAM_ACCESS_INTERN2_PASSWORD", "test-second-password")
	t.Setenv("TEAM_ACCESS_APPROVER_PASSWORD", "test-mentor-password")
	t.Setenv("TEAM_ACCESS_LISTEN", "")
	t.Setenv("TEAM_SELF_URL", "")
	for _, tc := range []struct{ role, file string }{{"team", "001_team.sql"}, {"rights", "001_rights.sql"}} {
		t.Setenv("TEAM_ACCESS_ROLE", tc.role)
		t.Setenv("TEAM_ACCESS_MIGRATION", filepath.Join("..", "..", "migrations", "team_access", tc.file))
		if err := run(ctx, serve); !errors.Is(err, stop) {
			t.Fatalf("%s startup: %v", tc.role, err)
		}
	}
}
