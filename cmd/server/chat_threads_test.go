package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestOriginalChatHistoryAndContext(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	for range 2 {
		if err := distributed.EnsureChatSchema(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `ALTER TABLE ai_jobs ADD COLUMN team_context jsonb`); err != nil {
		t.Fatal(err)
	}
	system := &distributed.System{DB: db}
	submit := func(question string) distributed.Job {
		t.Helper()
		job, err := system.SubmitChat(ctx, "alice:intern", "general", question, "")
		if err != nil {
			t.Fatal(err)
		}
		return job
	}
	first := submit("Original conversation")
	hidden := submit("Separate conversation")
	if _, err := db.Exec(ctx, `INSERT INTO ai_chat_threads(id, owner_id, title) VALUES($1,'alice:intern','Separate conversation')`, hidden.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE ai_jobs SET thread_id=id WHERE id=$1`, hidden.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE ai_jobs SET status='completed', answer='answer'`); err != nil {
		t.Fatal(err)
	}
	followUp := submit("Follow-up")
	turns, err := recentChatTurns(ctx, db, "alice:intern", followUp.ID)
	if err != nil || len(turns) != 1 || turns[0].Question != first.Query {
		t.Fatalf("unexpected context: %+v, %v", turns, err)
	}
	mux := http.NewServeMux()
	registerHistoryRoutes(mux, "", system)
	get := func(path, owner string, status int) []byte {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, owner)))
		if w.Code != status {
			t.Fatalf("GET %s: %d, want %d: %s", path, w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var history struct{ Turns []chatTurn }
	path := "/v1/chat/history"
	if err := json.Unmarshal(get(path, "alice", 200), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Turns) != 2 || history.Turns[0].ID != followUp.ID || history.Turns[1].ID != first.ID {
		t.Fatalf("unexpected history: %+v", history.Turns)
	}
	get(path+"?thread_id="+hidden.ID, "alice", 404)
	get("/v1/chat/threads", "alice", 404)
}
