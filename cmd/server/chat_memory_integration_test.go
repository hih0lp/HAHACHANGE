package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestOldChatMemoryIndexesAndRestoresOnlyOlderSameThread(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	if err := distributed.EnsureChatSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ALTER TABLE ai_jobs ADD COLUMN IF NOT EXISTS team_context jsonb`); err != nil {
		t.Fatal(err)
	}
	system := &distributed.System{DB: db}
	var ids []string
	for i := range 10 {
		job, err := system.SubmitChat(ctx, "owner:intern", "general", "question", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, job.ID)
		if _, err := db.Exec(ctx, `UPDATE ai_jobs SET status='completed', answer=$2, created_at=$3, completed_at=$3 WHERE id=$1`, job.ID, "answer", time.Date(2026, 11, 1, 0, i, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	current, err := system.SubmitChat(ctx, "owner:intern", "general", "remember first question", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE ai_jobs SET created_at=$2 WHERE id=$1`, current.ID, time.Date(2026, 11, 1, 0, 10, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	indexed := 0
	qdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/collections/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/points"):
			indexed++
			w.Write([]byte(`{"result":true}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/points/query"):
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": []any{map[string]any{"id": ids[0], "score": 0.9, "payload": map[string]any{"owner_id": "owner:intern"}}}}})
		default:
			w.Write([]byte(`{"result":true}`))
		}
	}))
	defer qdrant.Close()
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[[0.1,0.2]]`))
	}))
	defer embeddings.Close()
	service := rag.New(rag.Config{QdrantURL: qdrant.URL, EmbeddingsURL: embeddings.URL, MemoryCollectionName: "chat-memory"})
	if err := indexPendingChatMemory(ctx, db, service); err != nil {
		t.Fatal(err)
	}
	if indexed != 4 {
		t.Fatalf("indexed %d; expected bounded batch of four", indexed)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM ai_jobs WHERE memory_indexed_at IS NOT NULL`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("indexed jobs=%d err=%v", count, err)
	}
	turns, err := oldChatMemory(ctx, db, service, "owner:intern", current.ID, "first question")
	if err != nil || len(turns) != 1 || turns[0].Question != "question" {
		t.Fatalf("older turns=%+v err=%v", turns, err)
	}
	other, err := oldChatMemory(ctx, db, service, "other:intern", current.ID, "first question")
	if err != nil || len(other) != 0 {
		t.Fatalf("other owner got memory: %+v err=%v", other, err)
	}
}
