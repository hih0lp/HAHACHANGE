package distributed

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed chat.sql
var chatSchema string

func EnsureChatSchema(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(812610048)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, chatSchema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
