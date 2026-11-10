package teamaccess

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/segmentio/kafka-go"
)

func (s *Service) event(ctx context.Context, tx pgx.Tx, requestID, kind, subject, team, resource, cap string, revision int64) error {
	id := newID()
	payload, err := json.Marshal(map[string]any{
		"event_id": id, "type": kind, "occurred_at": now(), "subject_id": subject,
		"team_id": team, "resource_id": resource, "capability": cap, "request_id": requestID, "revision": revision,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ta_outbox(event_id,request_id,event_type,payload) VALUES($1,$2,$3,$4)`, id, requestID, kind, payload)
	return err
}

// PublishOutbox commits Kafka publication before marking the row delivered.
// A crash between those operations can redeliver an event with the same event_id.
func (s *Service) PublishOutbox(ctx context.Context) {
	if len(s.brokers) == 0 || s.brokers[0] == "" {
		log.Print("team-access outbox waiting: KAFKA_BROKERS is empty")
		return
	}
	writer := &kafka.Writer{Addr: kafka.TCP(s.brokers...), Topic: s.topic, Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll}
	defer writer.Close()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.publishOne(ctx, writer); err != nil {
				log.Printf("team-access outbox: %v", err)
			}
		}
	}
}
func (s *Service) publishOne(ctx context.Context, w *kafka.Writer) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, requestID string
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT event_id,request_id,payload FROM ta_outbox WHERE published_at IS NULL ORDER BY created_at,event_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &requestID, &payload)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err = w.WriteMessages(ctx, kafka.Message{Key: []byte(requestID), Value: payload}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE ta_outbox SET published_at=now() WHERE event_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
