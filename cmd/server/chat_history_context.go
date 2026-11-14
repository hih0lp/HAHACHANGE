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
	rows, err := db.Query(ctx, `SELECT left(previous.query,1000), left(previous.answer,3000)
		FROM ai_jobs AS previous JOIN ai_jobs AS current_job
		  ON previous.owner_id=current_job.owner_id
		  AND previous.thread_id IS NOT DISTINCT FROM current_job.thread_id
		WHERE current_job.id=$2 AND current_job.owner_id=$1 AND previous.status='completed'
		  AND (previous.created_at, previous.id) < (current_job.created_at, current_job.id)
		ORDER BY previous.created_at DESC, previous.id DESC LIMIT 8`, owner, jobID)
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
