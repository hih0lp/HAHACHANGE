package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hackchange/avito-rag/internal/rag"
)

func waitStatus(t *testing.T, jobs *jobManager, id, status string) jobSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := jobs.get(id); ok && job.Status == status {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, status)
	return jobSnapshot{}
}

func TestJobsRunInParallelAndQueueIsBounded(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	jobs := newJobManager(2, 1, time.Second, func(ctx context.Context, query string) (string, []rag.SearchHit, error) {
		started <- query
		select {
		case <-release:
			return "answer: " + query, nil, nil
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	})
	defer jobs.close()
	first, err := jobs.submit("first")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first worker did not start")
	}
	second, err := jobs.submit("second")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("second worker did not start")
	}
	waitStatus(t, jobs, first.ID, "running")
	waitStatus(t, jobs, second.ID, "running")
	third, err := jobs.submit("third")
	if err != nil {
		t.Fatal(err)
	}
	if job, ok := jobs.get(third.ID); !ok || job.Status != "queued" {
		t.Fatalf("third job: %+v, %v", job, ok)
	}
	if _, err := jobs.submit("fourth"); !errors.Is(err, errQueueFull) {
		t.Fatalf("queue error: %v", err)
	}
	close(release)
	for _, job := range []jobSnapshot{first, second, third} {
		if got := waitStatus(t, jobs, job.ID, "completed"); got.Answer != "answer: "+job.Query {
			t.Fatalf("answer: %+v", got)
		}
	}
}

func TestJobFailureAndSubscription(t *testing.T) {
	gate := make(chan struct{})
	jobs := newJobManager(1, 1, time.Second, func(context.Context, string) (string, []rag.SearchHit, error) {
		<-gate
		return "", nil, errors.New("LLM unavailable")
	})
	defer jobs.close()
	job, err := jobs.submit("question")
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe, ok := jobs.subscribe(job.ID)
	if !ok {
		t.Fatal("subscription missing")
	}
	defer unsubscribe()
	if initial := <-events; initial.ID != job.ID {
		t.Fatalf("initial: %+v", initial)
	}
	close(gate)
	if got := waitStatus(t, jobs, job.ID, "failed"); got.Error != "LLM unavailable" {
		t.Fatalf("failure: %+v", got)
	}
	select {
	case final := <-events:
		if final.Status != "failed" {
			t.Fatalf("event: %+v", final)
		}
	case <-time.After(time.Second):
		t.Fatal("no final event")
	}
	if _, _, ok := jobs.subscribe("missing"); ok {
		t.Fatal("missing job subscribed")
	}
}

func TestJobHTTPAndWebSocket(t *testing.T) {
	gate := make(chan struct{})
	jobs := newJobManager(1, 1, time.Second, func(context.Context, string) (string, []rag.SearchHit, error) {
		<-gate
		return "done", []rag.SearchHit{{Path: "rules.md"}}, nil
	})
	defer jobs.close()
	mux := http.NewServeMux()
	registerJobRoutes(mux, jobs, "secret")
	server := httptest.NewServer(mux)
	defer server.Close()
	if got := call(mux, "POST", "/v1/jobs", `{"query":"hello"}`, ""); got.Code != 401 {
		t.Fatalf("auth: %d", got.Code)
	}
	if got := call(mux, "POST", "/v1/jobs", `{"query":"x"}`, "secret"); got.Code != 400 {
		t.Fatalf("bad query: %d", got.Code)
	}
	created := call(mux, "POST", "/v1/jobs", `{"query":"hello"}`, "secret")
	if created.Code != 202 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var job jobSnapshot
	if err := json.Unmarshal(created.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if created.Header().Get("Location") != "/v1/jobs/"+job.ID {
		t.Fatal("missing location")
	}
	if got := call(mux, "GET", "/v1/jobs/missing", "", "secret"); got.Code != 404 {
		t.Fatalf("missing: %d", got.Code)
	}
	if got := call(mux, "GET", "/v1/jobs/"+job.ID, "", "secret"); got.Code != 200 {
		t.Fatalf("get: %d", got.Code)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/jobs/" + job.ID + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"api_key": "secret"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var event jobSnapshot
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if event.ID != job.ID {
		t.Fatalf("event: %+v", event)
	}
	close(gate)
	for event.Status != "completed" {
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
	}
	if event.Answer != "done" || len(event.Sources) != 1 {
		t.Fatalf("final: %+v", event)
	}
}

func TestJobLimitsAuthenticationAndReconnect(t *testing.T) {
	started := make(chan struct{}, 1)
	gate := make(chan struct{})
	jobs := newJobManager(1, 1, time.Second, func(context.Context, string) (string, []rag.SearchHit, error) {
		started <- struct{}{}
		<-gate
		return "ready", nil, nil
	})
	defer jobs.close()
	mux := http.NewServeMux()
	registerJobRoutes(mux, jobs, "secret")
	server := httptest.NewServer(mux)
	defer server.Close()
	if got := call(mux, "POST", "/v1/jobs", "{", "secret"); got.Code != 400 {
		t.Fatalf("json: %d", got.Code)
	}
	first := call(mux, "POST", "/v1/jobs", `{"query":"first"}`, "secret")
	if first.Code != 202 {
		t.Fatalf("first: %d", first.Code)
	}
	var job jobSnapshot
	if err := json.Unmarshal(first.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if got := call(mux, "POST", "/v1/jobs", `{"query":"second"}`, "secret"); got.Code != 202 {
		t.Fatalf("second: %d", got.Code)
	}
	if got := call(mux, "POST", "/v1/jobs", `{"query":"third"}`, "secret"); got.Code != 429 {
		t.Fatalf("queue: %d", got.Code)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/jobs/" + job.ID + "/ws"
	if _, response, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://evil.example"}}); err == nil || response == nil || response.StatusCode != 403 {
		t.Fatalf("origin: %v, %+v", err, response)
	}
	wrong, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = wrong.SetReadDeadline(time.Now().Add(time.Second))
	if err := wrong.WriteJSON(map[string]string{"api_key": "wrong"}); err != nil {
		t.Fatal(err)
	}
	var message jobSnapshot
	if err := wrong.ReadJSON(&message); err == nil {
		t.Fatal("wrong key received job")
	}
	wrong.Close()
	if got := call(mux, "GET", "/v1/jobs/"+job.ID, "", ""); got.Code != 401 {
		t.Fatalf("get auth: %d", got.Code)
	}
	close(gate)
	waitStatus(t, jobs, job.ID, "completed")
	reconnected, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	if err := reconnected.WriteJSON(map[string]string{"api_key": "secret"}); err != nil {
		t.Fatal(err)
	}
	_ = reconnected.SetReadDeadline(time.Now().Add(time.Second))
	if err := reconnected.ReadJSON(&message); err != nil || message.Status != "completed" {
		t.Fatalf("reconnect: %+v, %v", message, err)
	}
}

func TestJobDefaultsAndExpiry(t *testing.T) {
	jobs := newJobManager(0, 0, 0, func(context.Context, string) (string, []rag.SearchHit, error) { return "ok", nil, nil })
	defer jobs.close()
	job, err := jobs.submit("question")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, jobs, job.ID, "completed")
	jobs.mu.Lock()
	jobs.jobs[job.ID].finishedAt = time.Now().Add(-31 * time.Minute)
	jobs.pruneLocked(time.Now())
	jobs.mu.Unlock()
	if _, ok := jobs.get(job.ID); ok {
		t.Fatal("expired job was retained")
	}
}
