package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestJobRoutesStopEditAndHistory(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	if err := distributed.EnsureChatSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ALTER TABLE ai_jobs ADD COLUMN IF NOT EXISTS team_context jsonb`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAM_ACCESS_URL", "")
	mux := http.NewServeMux()
	system := &distributed.System{DB: db}
	registerDistributedJobRoutes(mux, system, "test-key")
	registerHistoryRoutes(mux, "test-key", system)

	call := func(method, path, owner, key, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-API-Key", key)
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req.WithContext(context.WithValue(req.Context(), ownerKey{}, owner)))
		if resp.Code != want {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, resp.Code, want, resp.Body.String())
		}
		return resp
	}
	const key = "test-key"
	const owner = "alice"
	const base = "/v1/jobs"
	call("POST", base, owner, "", `{"query":"Original question"}`, 401)
	for _, body := range []string{"{", `{"query":" "}`, `{"query":"Original question","scenario":"invalid"}`, `{"query":"Original question","thread_id":"other"}`} {
		call("POST", base, owner, key, body, 400)
	}
	created := call("POST", base, owner, key, `{"query":"Original question"}`, 202)
	path := created.Header().Get("Location")
	if !strings.HasPrefix(path, base+"/") {
		t.Fatalf("missing job location: %q", path)
	}
	call("GET", path, "bob", key, "", 404)
	call("POST", path+"/stop", "bob", key, "{}", 404)
	call("POST", path+"/edit", "bob", key, `{"query":"Foreign edit"}`, 404)
	call("POST", path+"/edit", owner, key, "{", 400)
	call("POST", path+"/edit", owner, key, `{"query":" "}`, 400)
	stopped := call("POST", path+"/stop", owner, key, "{}", 200)
	if !strings.Contains(stopped.Body.String(), "Остановлено пользователем") {
		t.Fatalf("expected cancellation: %s", stopped.Body.String())
	}
	edited := call("POST", path+"/edit", owner, key, `{"query":"  Edited question  "}`, 202)
	next := edited.Header().Get("Location")
	if next == path || !strings.HasPrefix(next, base+"/") {
		t.Fatalf("expected a new job: %q -> %q", path, next)
	}
	call("GET", next, owner, key, "", 200)
	call("POST", path+"/edit", owner, key, `{"query":"Duplicate edit"}`, 404)

	history := call("GET", "/v1/chat/history", owner, key, "", 200)
	var result struct{ Turns []chatTurn }
	if err := json.Unmarshal(history.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].Query != "Edited question" || result.Turns[0].Status != "queued" {
		t.Fatalf("unexpected chat after edit: %+v", result.Turns)
	}
}
