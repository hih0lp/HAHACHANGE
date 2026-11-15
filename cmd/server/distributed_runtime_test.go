package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDistributedStartupRequiresMigrationsAndServesHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopped := errors.New("listener closed")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/embed" {
			_, _ = w.Write([]byte("[[1,0]]"))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/points/count") {
			_, _ = w.Write([]byte(`{"result":{"count":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{}}`))
	}))
	defer backend.Close()
	cfg := config{RAG: rag.Config{EmbeddingsURL: backend.URL, QdrantURL: backend.URL, CollectionName: "startup"}}
	var calls int
	serve := func(addr string, handler http.Handler) error {
		calls++
		if addr != ":8000" {
			t.Error(addr)
		}
		result := httptest.NewRecorder()
		bootstrap := httptest.NewRecorder()
		handler.ServeHTTP(bootstrap, httptest.NewRequest("GET", "/", nil))
		request := httptest.NewRequest("GET", "/health", nil)
		for _, cookie := range bootstrap.Result().Cookies() {
			request.AddCookie(cookie)
		}
		handler.ServeHTTP(result, request)
		if result.Code != 200 || !strings.Contains(result.Body.String(), `"chunks":1`) {
			t.Errorf("health = %d %s", result.Code, result.Body)
		}
		return stopped
	}
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_SECRET", "isolated-test-session")
	if err := runDistributedContext(ctx, cfg, serve); err == nil || calls != 0 {
		t.Fatalf("missing database: %v", err)
	}
	t.Setenv("DATABASE_URL", "://invalid")
	if err := runDistributedContext(ctx, cfg, serve); err == nil || calls != 0 {
		t.Fatalf("invalid database: %v", err)
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL for distributed startup")
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	id, err := distributed.NewID()
	if err != nil {
		t.Fatal(err)
	}
	name := "startup_" + strings.ReplaceAll(id, "-", "")
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE "+identifier); err != nil {
			t.Error(err)
		}
	}()
	isolated, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	isolated.ConnConfig.Database = name
	isolatedDSN := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		address, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		address.Path = "/" + name
		isolatedDSN = address.String()
	}
	t.Setenv("DATABASE_URL", isolatedDSN)
	t.Setenv("APP_ROLE", "api")
	t.Setenv("TEAM_ACCESS_URL", "")
	if err := runDistributedContext(ctx, cfg, serve); err == nil || !strings.Contains(err.Error(), "job schema is missing") || calls != 0 {
		t.Fatalf("unmigrated database: %v", err)
	}
	db, err := pgxpool.NewWithConfig(ctx, isolated)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "migrations", "001_distributed_jobs.sql"))
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, string(schema))
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := runDistributedContext(ctx, cfg, serve); !errors.Is(err, stopped) || calls != 1 {
		t.Fatalf("distributed startup: %v, calls=%d", err, calls)
	}
}
