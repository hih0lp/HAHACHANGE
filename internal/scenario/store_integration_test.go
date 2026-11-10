package scenario

import (
	"context"
	"errors"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestStoreOwnershipAndConcurrentRevision(t *testing.T) {
	db := testsupport.Postgres(t)
	s := Store{DB: db}
	ctx := context.Background()
	original := State{ID: "11111111-1111-4111-8111-111111111111", OwnerID: "alice", Revision: 1, SchemaVersion: 1, Status: "needs_input"}
	if err := s.Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "bob", original.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign scenario readable: %v", err)
	}
	first, err := s.Get(ctx, "alice", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	first.Status = "waiting_access"
	saved, err := s.Save(ctx, first)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	second.Status = "ready_for_task"
	if _, err = s.Save(ctx, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale overwrite accepted: %v", err)
	}
	// A fresh Store instance observes committed state, independent of API memory.
	restored, err := (Store{DB: db}).Get(ctx, "alice", original.ID)
	if err != nil || restored.Status != "waiting_access" || restored.OwnerID != "alice" {
		t.Fatalf("lost persisted state: %+v %v", restored, err)
	}
	db.Close()
	if err = s.Create(ctx, original); err == nil {
		t.Fatal("closed database accepted create")
	}
	if _, err = s.Save(ctx, restored); err == nil {
		t.Fatal("closed database accepted update")
	}
	if _, err = s.Get(ctx, "alice", original.ID); err == nil {
		t.Fatal("closed database accepted read")
	}
}
