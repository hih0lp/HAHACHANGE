package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestJobWebSocketStreamsCompletionToOwner(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	if err := distributed.EnsureChatSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ALTER TABLE ai_jobs ADD COLUMN IF NOT EXISTS team_context jsonb`); err != nil {
		t.Fatal(err)
	}
	system := &distributed.System{DB: db}
	job, err := system.SubmitChat(ctx, "browser:intern", "general", "Question", "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerDistributedJobRoutes(mux, system, "api-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, "browser")))
	}))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/jobs/" + job.ID + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"api_key": "api-key"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	var first distributed.Job
	if err := conn.ReadJSON(&first); err != nil || first.Status != "queued" {
		t.Fatalf("first job=%+v err=%v", first, err)
	}
	if _, err := db.Exec(ctx, `UPDATE ai_jobs SET status='completed', answer='Finished', completed_at=now() WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	var final distributed.Job
	if err := conn.ReadJSON(&final); err != nil || final.Status != "completed" || final.Answer != "Finished" {
		t.Fatalf("final job=%+v err=%v", final, err)
	}
}
