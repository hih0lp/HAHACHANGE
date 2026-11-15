package distributed_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"github.com/jackc/pgx/v5"
)

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
