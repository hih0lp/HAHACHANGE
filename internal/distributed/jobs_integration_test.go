package distributed_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"github.com/jackc/pgx/v5"
)

func TestSubmitContextAndExpiredLeaseRecovery(t *testing.T) {
	s, ctx := jobSystem(t)
	job, err := s.SubmitWithContext(ctx, "browser:intern", "general", "context question", json.RawMessage(`{"jira_account_id":"first"}`))
	if err != nil {
		t.Fatal(err)
	}
	var account string
	if err := s.DB.QueryRow(ctx, `SELECT team_context->>'jira_account_id' FROM ai_jobs WHERE id=$1`, job.ID).Scan(&account); err != nil || account != "first" {
		t.Fatalf("stored team context=%q err=%v", account, err)
	}
	if err := s.RequeueExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE ai_jobs SET status='running', lease_until=now()-interval '1 minute' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE ai_job_outbox SET published_at=now() WHERE job_id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RequeueExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var published bool
	if err := s.DB.QueryRow(ctx, `SELECT j.status, o.published_at IS NOT NULL FROM ai_jobs j JOIN ai_job_outbox o ON o.job_id=j.id WHERE j.id=$1`, job.ID).Scan(&status, &published); err != nil || status != "queued" || published {
		t.Fatalf("lease recovery: status=%q published=%v err=%v", status, published, err)
	}
	if err := s.PublishPending(ctx); err == nil {
		t.Fatal("missing Kafka writer was accepted")
	}
	if err := s.Work(ctx, func(context.Context, string) (string, any, error) { return "", nil, nil }, time.Second); err == nil {
		t.Fatal("missing Kafka reader was accepted")
	}
	if _, err := s.Submit(ctx, "browser:intern", "general", "another question"); err != nil {
		t.Fatal(err)
	}
}

func jobSystem(t *testing.T) (*distributed.System, context.Context) {
	t.Helper()
	db := testsupport.Postgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	t.Cleanup(cancel)
	if err := distributed.EnsureChatSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ALTER TABLE ai_jobs ADD COLUMN IF NOT EXISTS team_context jsonb`); err != nil {
		t.Fatal(err)
	}
	return &distributed.System{DB: db, MaxTries: 2}, ctx
}

func TestStopCancelsWorkerHTTPRequestAndEditQueuesReplacement(t *testing.T) {
	s, ctx := jobSystem(t)
	const owner = "browser:intern"
	job, err := s.SubmitChat(ctx, owner, "general", "original", "")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	cancelled := make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-ctx.Done():
		}
	}))
	defer model.Close()
	worked := make(chan error, 1)
	go func() {
		worked <- s.ProcessOne(ctx, job.ID, func(work context.Context, query string) (string, any, error) {
			req, err := http.NewRequestWithContext(work, http.MethodGet, model.URL, nil)
			if err != nil {
				return "", nil, err
			}
			resp, err := model.Client().Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
			return "late answer", nil, err
		}, 10*time.Second)
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("worker did not start the model request")
	}
	if _, err := s.Stop(ctx, "other:intern", job.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign stop: %v", err)
	}
	stopped, err := s.Stop(ctx, owner, job.ID)
	if err != nil || stopped.Status != "failed" || stopped.Error != "Остановлено пользователем" {
		t.Fatalf("stop: %+v, %v", stopped, err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not cancel the HTTP request before the worker timeout")
	}
	if err = <-worked; err != nil {
		t.Fatal(err)
	}
	stopped, err = s.Get(ctx, owner, job.ID)
	if err != nil || stopped.Status != "failed" || stopped.Answer != "" {
		t.Fatalf("late result changed stopped job: %+v, %v", stopped, err)
	}
	if _, err := s.EditChat(ctx, "other:intern", job.ID, "foreign edit", ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign edit: %v", err)
	}
	edited, err := s.EditChat(ctx, owner, job.ID, "edited", "")
	if err != nil || edited.ID == job.ID || edited.Query != "edited" || edited.Status != "queued" {
		t.Fatalf("edit: %+v, %v", edited, err)
	}
	if _, err := s.EditChat(ctx, owner, job.ID, "repeat", ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("superseded job accepted another edit: %v", err)
	}
	if err = s.ProcessOne(ctx, edited.ID, func(work context.Context, query string) (string, any, error) {
		user, id := distributed.ChatJob(work)
		if user != owner || id != edited.ID || query != edited.Query {
			t.Fatalf("worker received incorrect job: %s %s %s", user, id, query)
		}
		return "edited answer", []string{}, nil
	}, time.Second); err != nil {
		t.Fatal(err)
	}
	result, err := s.Get(ctx, owner, edited.ID)
	if err != nil || result.Status != "completed" || result.Answer != "edited answer" {
		t.Fatalf("edited result: %+v, %v", result, err)
	}
}

func TestWorkerRetriesBeforeTerminalFailure(t *testing.T) {
	s, ctx := jobSystem(t)
	job, err := s.SubmitChat(ctx, "browser:intern", "general", "retry", "")
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	answer := func(context.Context, string) (string, any, error) {
		attempts++
		return "", nil, errors.New("model unavailable")
	}
	for _, want := range []string{"queued", "failed", "failed"} {
		if err = s.ProcessOne(ctx, job.ID, answer, time.Second); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, "browser:intern", job.ID)
		if err != nil || got.Status != want {
			t.Fatalf("expected %s, got %+v (%v)", want, got, err)
		}
	}
	if attempts != 2 {
		t.Fatalf("expected exactly two attempts, got %d", attempts)
	}
}
