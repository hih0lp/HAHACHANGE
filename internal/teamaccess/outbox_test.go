package teamaccess

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hackchange/avito-rag/internal/testsupport"
	"github.com/segmentio/kafka-go"
)

type publishEvents func(context.Context, ...kafka.Message) error

func (f publishEvents) WriteMessages(ctx context.Context, messages ...kafka.Message) error {
	return f(ctx, messages...)
}

func TestAccessOutboxKeepsEventsPendingUntilBrokerAcknowledges(t *testing.T) {
	ctx := context.Background()
	db := testsupport.Postgres(t)
	if _, err := db.Exec(ctx, `CREATE TABLE ta_outbox(event_id text PRIMARY KEY,request_id text,event_type text,payload jsonb,created_at timestamptz DEFAULT now(),published_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	s := Service{DB: db}
	brokerDown := errors.New("broker unavailable")
	count := 0
	publisher := publishEvents(func(_ context.Context, messages ...kafka.Message) error {
		count++
		if len(messages) != 1 || string(messages[0].Key) != "request-1" {
			t.Fatalf("messages: %+v", messages)
		}
		var event map[string]any
		if err := json.Unmarshal(messages[0].Value, &event); err != nil || event["type"] != "access.approved" || event["subject_id"] != "intern" {
			t.Fatalf("payload=%s err=%v", messages[0].Value, err)
		}
		if count == 1 {
			return brokerDown
		}
		return nil
	})
	if err := s.publishOne(ctx, publisher); err != nil || count != 0 {
		t.Fatalf("empty outbox: count=%d err=%v", count, err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := s.event(ctx, tx, "request-1", "access.approved", "intern", "demo", "catalogue", "read", 2); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.publishOne(ctx, publisher); !errors.Is(err, brokerDown) {
		t.Fatalf("unacknowledged: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := s.publishOne(ctx, publisher); err != nil {
			t.Fatal(err)
		}
	}
	var published bool
	if err := db.QueryRow(ctx, `SELECT published_at IS NOT NULL FROM ta_outbox`).Scan(&published); err != nil || !published || count != 2 {
		t.Fatalf("published=%t count=%d err=%v", published, count, err)
	}
}
