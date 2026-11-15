package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestRunSeedsMessengerBeforeServing(t *testing.T) {
	t.Setenv("CHAT_DATABASE_URL", "")
	t.Setenv("CHAT_TEAM_ACCESS_URL", "http://127.0.0.1:1")
	t.Setenv("CHAT_EMBEDDINGS_URL", "http://127.0.0.1:1")
	stop := errors.New("listener closed")
	serve := func(server *http.Server) error {
		if server.Addr != ":8080" {
			t.Errorf("address = %s", server.Addr)
		}
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
		if response.Code != 200 {
			t.Errorf("messenger page = %d", response.Code)
		}
		return stop
	}
	if err := run(context.Background(), serve); err == nil || errors.Is(err, stop) {
		t.Fatalf("missing DSN: %v", err)
	}
	t.Setenv("CHAT_DATABASE_URL", "://invalid")
	if err := run(context.Background(), serve); err == nil || errors.Is(err, stop) {
		t.Fatalf("invalid DSN: %v", err)
	}
	db := testsupport.Postgres(t)
	tei := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Inputs []string }
		if r.URL.Path != "/embed" || json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "request", 400)
			return
		}
		vectors := make([][]float32, len(in.Inputs))
		for i := range vectors {
			vectors[i] = make([]float32, 384)
			vectors[i][0] = 1
		}
		json.NewEncoder(w).Encode(vectors)
	}))
	defer tei.Close()
	t.Setenv("CHAT_DATABASE_URL", testsupport.IsolatedURL(db))
	t.Setenv("CHAT_EMBEDDINGS_URL", tei.URL)
	t.Setenv("CHAT_PORT", "")
	if err := run(context.Background(), serve); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM chat_messages").Scan(&count); err != nil || count != 10 {
		t.Fatalf("persisted seed: %d %v", count, err)
	}
}
