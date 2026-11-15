package distributed_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

type testDelivery struct {
	pending, committed    []kafka.Message
	publishErr, commitErr error
}

func (q *testDelivery) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	if q.publishErr != nil {
		return q.publishErr
	}
	q.pending = append(q.pending, msgs...)
	return nil
}

func (q *testDelivery) FetchMessage(context.Context) (kafka.Message, error) {
	if len(q.pending) == 0 {
		return kafka.Message{}, io.EOF
	}
	message := q.pending[0]
	q.pending = q.pending[1:]
	return message, nil
}

func (q *testDelivery) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	if q.commitErr != nil {
		return q.commitErr
	}
	q.committed = append(q.committed, msgs...)
	return nil
}

func TestOutboxRetriesThenWorkerCommitsDuplicateSafely(t *testing.T) {
	s, ctx := jobSystem(t)
	backendDown := errors.New("broker unavailable")
	queue := &testDelivery{publishErr: backendDown}
	s.Writer, s.Reader = queue, queue
	owner := "browser:intern"
	job, err := s.SubmitWithContext(ctx, owner, "general", "question", json.RawMessage(`{"jira_account_id":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishPending(ctx); !errors.Is(err, backendDown) {
		t.Fatalf("failed publication: %v", err)
	}
	queue.publishErr = nil
	for i := 0; i < 2; i++ {
		if err := s.PublishPending(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(queue.pending) != 1 || string(queue.pending[0].Key) != job.ID {
		t.Fatalf("queued messages = %+v", queue.pending)
	}
	queue.pending = append(queue.pending, queue.pending[0])
	modelCalls := 0
	model := func(work context.Context, query string) (string, any, error) {
		modelCalls++
		gotOwner, gotID := distributed.ChatJob(work)
		if gotOwner != owner || gotID != job.ID || query != job.Query || !strings.Contains(distributed.TeamContext(work), "jira_account_id") {
			t.Error("worker lost the job identity or team context")
		}
		return "answer", []string{"source"}, nil
	}
	queue.commitErr = backendDown
	if err := s.Work(ctx, model, time.Second); !errors.Is(err, backendDown) {
		t.Fatalf("failed commit: %v", err)
	}
	queue.commitErr = nil
	if err := s.Work(ctx, model, time.Second); !errors.Is(err, io.EOF) {
		t.Fatalf("drained worker: %v", err)
	}
	got, err := s.Get(ctx, owner, job.ID)
	if err != nil || got.Answer != "answer" || got.Status != "completed" || modelCalls != 1 || len(queue.committed) != 1 {
		t.Fatalf("delivered job=%+v model calls=%d committed=%v err=%v", got, modelCalls, queue.committed, err)
	}
}

func TestTeamTokenLivesOnlyInRedisAndIsRemovedAfterAnswer(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR for Redis integration")
	}
	s, ctx := jobSystem(t)
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	s.Redis = client
	const owner = "browser:intern"
	job, err := s.Submit(ctx, owner, "general", "verify token scope")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StoreTeamToken(ctx, job.ID, "ephemeral-test-session"); err != nil {
		t.Fatal(err)
	}
	key := "ai-job-team-token:" + job.ID
	defer client.Del(context.Background(), key)
	if ttl := client.TTL(ctx, key).Val(); ttl < 14*time.Minute || ttl > 15*time.Minute {
		t.Fatalf("token TTL = %s", ttl)
	}
	if err := s.ProcessOne(ctx, job.ID, func(work context.Context, _ string) (string, any, error) {
		if token := distributed.TeamToken(work); token != "ephemeral-test-session" {
			t.Error("missing job session")
		}
		status, err := s.Get(work, owner, job.ID)
		if err != nil || status.Status != "running" {
			t.Errorf("status while running: %+v %v", status, err)
		}
		return "finished", nil, nil
	}, time.Second); err != nil {
		t.Fatal(err)
	}
	if n := client.Exists(ctx, key).Val(); n != 0 {
		t.Fatal("team session survived job completion")
	}
}
