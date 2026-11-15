package mockfixture

import (
	"context"
	"testing"

	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestOpenDatabaseCreatesSharedMockStorage(t *testing.T) {
	ctx := context.Background()
	if db, err := OpenDatabase(ctx, ""); err != nil || db != nil {
		t.Fatalf("disabled database: db=%v err=%v", db, err)
	}
	if _, err := OpenDatabase(ctx, "://bad-url"); err == nil {
		t.Fatal("invalid database URL accepted")
	}
	isolated := testsupport.Postgres(t)
	db, err := OpenDatabase(ctx, isolated.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ready bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('bobby_mock_datasets') IS NOT NULL`).Scan(&ready); err != nil || !ready {
		t.Fatalf("mock storage was not created: ready=%v err=%v", ready, err)
	}
	if err := createTable(ctx, db); err != nil {
		t.Fatalf("repeating schema initialization failed: %v", err)
	}
}
