package distributed

import (
	"context"
	"errors"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"github.com/segmentio/kafka-go"
	"sync"
	"testing"
	"time"
)

type brokerFixture struct {
	messages                        []kafka.Message
	writes, commits                 int
	writeErr, commitErr, errorFetch error
}

func (b *brokerFixture) WriteMessages(ctx context.Context, m ...kafka.Message) error {
	b.writes++
	if b.writeErr != nil {
		return b.writeErr
	}
	b.messages = append(b.messages, m...)
	return nil
}
func (b *brokerFixture) FetchMessage(context.Context) (kafka.Message, error) {
	if len(b.messages) == 0 {
		if b.errorFetch != nil {
			return kafka.Message{}, b.errorFetch
		}
		return kafka.Message{}, context.Canceled
	}
	v := b.messages[0]
	b.messages = b.messages[1:]
	return v, nil
}
func (b *brokerFixture) CommitMessages(context.Context, ...kafka.Message) error {
	b.commits++
	return b.commitErr
}

func TestDurableJobLifecycle(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	broker := &brokerFixture{}
	s := System{DB: db, Writer: broker, Reader: broker, MaxTries: 2}
	j, err := s.Submit(ctx, "alice", "general", "question")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, "bob", j.ID); err == nil {
		t.Fatal("foreign owner saw job")
	}
	if err = s.PublishPending(ctx); err != nil || broker.writes != 1 {
		t.Fatalf("publish %v %+v", err, broker)
	}
	if err = s.PublishPending(ctx); err != nil || broker.writes != 1 {
		t.Fatal("published outbox twice without retry")
	}
	calls := 0
	answer := func(context.Context, string) (string, any, error) { calls++; return "answer", []string{"source"}, nil }
	if err = s.Work(ctx, answer, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if broker.commits != 1 || calls != 1 {
		t.Fatal("message not committed after work")
	}
	if err = s.ProcessOne(ctx, j.ID, answer, time.Second); err != nil || calls != 1 {
		t.Fatal("duplicate delivery called model")
	}
	saved, err := s.Get(ctx, "alice", j.ID)
	if err != nil || saved.Status != "completed" || saved.Answer != "answer" {
		t.Fatalf("lost answer: %+v %v", saved, err)
	}
}

func TestRetryRecoveryAndLateWorker(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	s := System{DB: db, MaxTries: 2}
	j, err := s.Submit(ctx, "a", "general", "q")
	if err != nil {
		t.Fatal(err)
	}
	fail := func(context.Context, string) (string, any, error) { return "", nil, errors.New("model unavailable") }
	if err = s.ProcessOne(ctx, j.ID, fail, time.Second); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(ctx, "a", j.ID)
	if v.Status != "queued" {
		t.Fatal("retry not queued")
	}
	if err = s.ProcessOne(ctx, j.ID, fail, time.Second); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Get(ctx, "a", j.ID)
	if v.Status != "failed" || v.Error == "" {
		t.Fatal("retry exhaustion not terminal")
	}
	j, err = s.Submit(ctx, "a", "general", "recover")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.ProcessOne(ctx, j.ID, func(context.Context, string) (string, any, error) {
			close(started)
			<-release
			return "stale", nil, nil
		}, time.Second); err != nil {
			t.Error(err)
		}
	}()
	<-started
	if _, err = db.Exec(ctx, `UPDATE ai_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, j.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.RequeueExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessOne(ctx, j.ID, func(context.Context, string) (string, any, error) { return "fresh", nil, nil }, time.Second); err != nil {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	v, _ = s.Get(ctx, "a", j.ID)
	if v.Answer != "fresh" {
		t.Fatal("late worker overwrote recovered result")
	}
}

func TestOutboxAndWorkerFailures(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	s := System{DB: db, MaxTries: 1}
	j, err := s.Submit(ctx, "a", "general", "q")
	if err != nil {
		t.Fatal(err)
	}
	if s.PublishPending(ctx) == nil || s.Work(ctx, nil, time.Second) == nil {
		t.Fatal("unconfigured broker accepted")
	}
	broker := &brokerFixture{writeErr: errors.New("broker down")}
	s.Writer = broker
	if s.PublishPending(ctx) == nil {
		t.Fatal("broker error swallowed")
	}
	var pending int
	db.QueryRow(ctx, `SELECT count(*) FROM ai_job_outbox WHERE published_at IS NULL`).Scan(&pending)
	if pending != 1 {
		t.Fatal("outbox lost on failed send")
	}
	broker.writeErr = nil
	if err = s.PublishPending(ctx); err != nil {
		t.Fatal(err)
	}
	s.Reader = broker
	broker.commitErr = errors.New("commit failed")
	answer := func(context.Context, string) (string, any, error) { return "ok", nil, nil }
	if s.Work(ctx, answer, time.Second) == nil {
		t.Fatal("commit error swallowed")
	}
	j2, _ := s.Submit(ctx, "a", "general", "invalid result")
	if s.ProcessOne(ctx, j2.ID, func(context.Context, string) (string, any, error) { return "", make(chan int), nil }, time.Second) == nil {
		t.Fatal("nonserializable result accepted")
	}
	db.Close()
	if _, err = s.Submit(ctx, "a", "general", "q"); err == nil {
		t.Fatal("closed database accepted submit")
	}
	if _, err = s.Get(ctx, "a", j.ID); err == nil {
		t.Fatal("closed database accepted read")
	}
	if s.PublishPending(ctx) == nil || s.RequeueExpired(ctx) == nil || s.ProcessOne(ctx, j.ID, answer, time.Second) == nil {
		t.Fatal("database failures swallowed")
	}
	broker.messages = []kafka.Message{{Value: []byte(j.ID)}}
	if s.Work(ctx, answer, time.Second) == nil {
		t.Fatal("processing failure committed")
	}
}

func TestSubmitRollsBackWhenOutboxUnavailable(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	s := System{DB: db}
	if _, err := db.Exec(ctx, `DROP TABLE ai_job_outbox`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctx, "owner", "general", "question"); err == nil {
		t.Fatal("job accepted without durable outbox")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM ai_jobs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("job persisted without event")
	}
	s.Writer = &brokerFixture{}
	if s.PublishPending(ctx) == nil {
		t.Fatal("missing outbox ignored")
	}
	if _, err := db.Exec(ctx, `DROP TABLE ai_actions; DROP TABLE ai_jobs`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctx, "owner", "general", "question"); err == nil {
		t.Fatal("missing jobs table ignored")
	}
}
