package main

import (
	"context"
	"errors"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDistributedStartupValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_SECRET", "")
	if runDistributed(config{}) == nil {
		t.Fatal("missing configuration accepted")
	}
	t.Setenv("SESSION_SECRET", "test")
	t.Setenv("DATABASE_URL", "://invalid")
	if runDistributed(config{}) == nil {
		t.Fatal("bad database DSN accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://u@127.0.0.1:1/d?sslmode=disable&connect_timeout=1")
	if runDistributed(config{}) == nil {
		t.Fatal("unreachable database accepted")
	}
}

func TestDistributedStartupAPIAndWorker(t *testing.T) {
	db := testsupport.Postgres(t)
	address, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	q := address.Query()
	q.Set("search_path", db.Config().ConnConfig.RuntimeParams["search_path"])
	address.RawQuery = q.Encode()
	t.Setenv("DATABASE_URL", address.String())
	t.Setenv("SESSION_SECRET", "secret")
	t.Setenv("TEAM_ACCESS_URL", "http://127.0.0.1:1")
	t.Setenv("KAFKA_BROKERS", "127.0.0.1:1")
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/embed" {
			w.Write([]byte(`[[1,2]]`))
		} else {
			w.Write([]byte(`{"result":{"count":1}}`))
		}
	}))
	defer upstream.Close()
	cfg := config{IndexOnStartup: true, RAG: rag.Config{QdrantURL: upstream.URL, EmbeddingsURL: upstream.URL, CollectionName: "test", DocumentsPath: t.TempDir()}}
	old := listenAndServe
	t.Cleanup(func() { listenAndServe = old })
	want := errors.New("listener stopped")
	listenAndServe = func(_ string, h http.Handler) error {
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest("GET", "/onboarding", nil))
		if out.Code != 200 {
			t.Fatal("scenario route not registered")
		}
		return want
	}
	if err := runDistributed(cfg); !errors.Is(err, want) {
		t.Fatal(err)
	}
	t.Setenv("APP_ROLE", "worker")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := runDistributedContext(ctx, cfg); err == nil {
		t.Fatal("canceled worker did not stop")
	}
	t.Setenv("APP_ROLE", "")
	cfg.RAG.DocumentsPath = t.TempDir()
	cfg.RAG.ChunkSize = 80
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/points/count") {
			http.Error(w, "failed count", 500)
			return
		}
		if r.URL.Path == "/embed" {
			w.Write([]byte(`[[1,2]]`))
		} else {
			w.Write([]byte(`{"result":{}}`))
		}
	}))
	defer broken.Close()
	cfg.RAG.QdrantURL = broken.URL
	cfg.RAG.EmbeddingsURL = broken.URL
	if err := runDistributed(cfg); err == nil || !strings.Contains(err.Error(), "initial indexing") {
		t.Fatalf("indexing error: %v", err)
	}
	q.Set("search_path", "nonexistent_test_schema")
	address.RawQuery = q.Encode()
	t.Setenv("DATABASE_URL", address.String())
	if err := runDistributed(config{}); err == nil || !strings.Contains(err.Error(), "schema is missing") {
		t.Fatalf("missing migration accepted: %v", err)
	}
}
