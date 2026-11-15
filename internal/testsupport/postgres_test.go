package testsupport

import (
	"context"
	"testing"
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
}
