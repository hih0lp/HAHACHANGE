package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type chatContextTurn struct {
	Question string
	Answer   string
}

// Read only this browser owner's completed turns before the current job.
// Bound both turn count and text size so a long chat cannot consume the model
// context. The most recent eight turns are restored in chronological order.
func recentChatTurns(ctx context.Context, db *pgxpool.Pool, owner, jobID string) ([]chatContextTurn, error) {
	rows, err := db.Query(ctx, `SELECT left(query,1000), left(answer,3000)
		FROM ai_jobs
		WHERE owner_id=$1 AND status='completed'
		  AND created_at < (SELECT created_at FROM ai_jobs WHERE id=$2 AND owner_id=$1)
		ORDER BY created_at DESC, id DESC LIMIT 8`, owner, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns := []chatContextTurn{}
	for rows.Next() {
		var turn chatContextTurn
		if err := rows.Scan(&turn.Question, &turn.Answer); err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, nil
}
