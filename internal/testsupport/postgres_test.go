package testsupport

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresCreatesIsolatedSchema(t *testing.T) {
	first := Postgres(t)
	second := Postgres(t)
	ctx := context.Background()
	if _, err := first.Exec(ctx, `CREATE TABLE isolation_probe (id integer)`); err != nil {
		t.Fatal(err)
	}
	var found bool
	if err := second.QueryRow(ctx, `SELECT to_regclass('isolation_probe') IS NOT NULL`).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("test databases share a schema")
	}
	third, err := pgxpool.New(ctx, IsolatedURL(first))
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	if err := third.QueryRow(ctx, `SELECT to_regclass('isolation_probe') IS NOT NULL`).Scan(&found); err != nil || !found {
		t.Fatalf("isolated URL lost schema: found=%v err=%v", found, err)
	}
}
