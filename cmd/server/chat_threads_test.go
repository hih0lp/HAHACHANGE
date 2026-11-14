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

func TestChatThreadHistoryAndContext(t *testing.T) {
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
	submit := func(thread, question string) distributed.Job {
		t.Helper()
		job, err := system.SubmitChat(ctx, "alice:intern", "general", question, thread, "")
		if err != nil {
			t.Fatal(err)
		}
		return job
	}
	submit("", "Old conversation")
	first := submit("new", "First conversation")
	second := submit("new", "Second conversation")
	if first.ThreadID == "" || second.ThreadID == "" || first.ThreadID == second.ThreadID {
		t.Fatalf("unexpected thread IDs: %q, %q", first.ThreadID, second.ThreadID)
	}
	if _, err := db.Exec(ctx, `UPDATE ai_jobs SET status='completed', answer='answer'`); err != nil {
		t.Fatal(err)
	}
	followUp := submit(first.ThreadID, "Follow-up")
	turns, err := recentChatTurns(ctx, db, "alice:intern", followUp.ID)
	if err != nil || len(turns) != 1 || turns[0].Question != first.Query {
		t.Fatalf("unexpected context: %+v, %v", turns, err)
	}
	if _, err := system.SubmitChat(ctx, "alice:mentor", "general", "wrong role", first.ThreadID, ""); err == nil {
		t.Fatal("submission to another role's thread succeeded")
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
	var list struct{ Threads []chatThread }
	if err := json.Unmarshal(get("/v1/chat/threads", "alice", 200), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Threads) != 3 || list.Threads[0].ID != first.ThreadID || list.Threads[2].ID != "legacy" {
		t.Fatalf("unexpected thread list: %+v", list.Threads)
	}
	var history struct{ Turns []chatTurn }
	path := "/v1/chat/history?thread_id=" + first.ThreadID
	if err := json.Unmarshal(get(path, "alice", 200), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Turns) != 2 || history.Turns[0].ID != followUp.ID || history.Turns[1].ID != first.ID {
		t.Fatalf("unexpected history: %+v", history.Turns)
	}
	get(path, "bob", 404)
}
