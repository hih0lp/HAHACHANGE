package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestTeamDecisionEventRefreshesOnlyMatchingWaitingScenario(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE ai_scenario_events (event_id text PRIMARY KEY, received_at timestamptz DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	store := scenario.Store{DB: db}
	state := scenario.State{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", OwnerID: "intern", Revision: 1, Status: "waiting_access", Widget: scenario.Widget{RequestID: "request-a"}}
	if err := store.Create(ctx, state); err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{
		[]byte("{"),
		[]byte(`{"event_id":"","subject_id":"intern"}`),
	} {
		if err := applyTeamEvent(ctx, db, payload); err == nil && string(payload) == "{" {
			t.Fatal("invalid event accepted")
		}
	}
	event, _ := json.Marshal(teamEvent{EventID: "event-1", Type: "access.approved", SubjectID: "intern", RequestID: "request-a"})
	for range 2 {
		if err := applyTeamEvent(ctx, db, event); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Get(ctx, "intern", state.ID)
	if err != nil || got.Status != "needs_refresh" || got.Revision != 2 {
		t.Fatalf("event result=%+v err=%v", got, err)
	}
	unrelated, _ := json.Marshal(teamEvent{EventID: "event-2", Type: "access.rejected", SubjectID: "other", RequestID: "request-a"})
	if err := applyTeamEvent(ctx, db, unrelated); err != nil {
		t.Fatal(err)
	}
	got, err = store.Get(ctx, "intern", state.ID)
	if err != nil || got.Revision != 2 {
		t.Fatalf("unrelated event changed scenario=%+v err=%v", got, err)
	}
}
