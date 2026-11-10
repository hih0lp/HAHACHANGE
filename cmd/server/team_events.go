package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
)

type teamEvent struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	SubjectID string `json:"subject_id"`
	RequestID string `json:"request_id"`
}

func consumeTeamEvents(ctx context.Context, db *pgxpool.Pool, brokers []string) {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: "team-access-events", GroupID: "ai-buddy-team-access", StartOffset: kafka.FirstOffset, MinBytes: 1, MaxBytes: 1 << 20})
	defer reader.Close()
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("team event fetch: %v", err)
			}
			return
		}
		if err = applyTeamEvent(ctx, db, message.Value); err != nil {
			log.Printf("team event apply: %v", err)
			time.Sleep(time.Second)
			continue
		}
		if err = reader.CommitMessages(ctx, message); err != nil {
			log.Printf("team event commit: %v", err)
		}
	}
}
func applyTeamEvent(ctx context.Context, db *pgxpool.Pool, data []byte) error {
	var event teamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}
	if event.EventID == "" || event.SubjectID == "" {
		return nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO ai_scenario_events(event_id) VALUES($1) ON CONFLICT DO NOTHING`, event.EventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if event.RequestID != "" && (event.Type == "access.approved" || event.Type == "access.rejected") {
		_, err = tx.Exec(ctx, `UPDATE ai_scenarios SET revision=revision+1,updated_at=now(),
    state=jsonb_set(jsonb_set(jsonb_set(state,'{status}',to_jsonb('needs_refresh'::text)),
      '{revision}',to_jsonb(revision+1)),
      '{message}',to_jsonb('Получено решение по заявке. Проверяем актуальные права.'::text))
    WHERE owner_id=$1 AND state->'widget'->>'request_id'=$2 AND state->>'status'='waiting_access'`, event.SubjectID, event.RequestID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
