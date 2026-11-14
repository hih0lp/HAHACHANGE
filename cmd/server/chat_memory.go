package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type oldChatTurn struct {
	AskedAt  time.Time `json:"asked_at"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
}

func indexPendingChatMemory(ctx context.Context, db *pgxpool.Pool, service *rag.Service) error {
	rows, err := db.Query(ctx, `SELECT id::text, owner_id, created_at, left(query,1000), left(answer,3000)
		FROM ai_jobs WHERE status='completed' AND memory_indexed_at IS NULL AND answer IS NOT NULL
		ORDER BY completed_at, id LIMIT 4`)
	if err != nil {
		return err
	}
	type record struct {
		id, owner, question, answer string
		createdAt                   time.Time
	}
	var pending []record
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.id, &item.owner, &item.createdAt, &item.question, &item.answer); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range pending {
		if strings.TrimSpace(item.question) != "" && strings.TrimSpace(item.answer) != "" {
			if err := service.UpsertMemory(ctx, item.id, item.owner, item.createdAt, item.question+"\n"+item.answer); err != nil {
				return err
			}
		}
		if _, err := db.Exec(ctx, `UPDATE ai_jobs SET memory_indexed_at=now() WHERE id=$1 AND status='completed'`, item.id); err != nil {
			return err
		}
	}
	return nil
}

func runChatMemoryIndexer(ctx context.Context, db *pgxpool.Pool, service *rag.Service) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 45*time.Second)
		if err := indexPendingChatMemory(work, db, service); err != nil && ctx.Err() == nil {
			log.Printf("chat memory indexing: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func oldChatMemory(ctx context.Context, db *pgxpool.Pool, service *rag.Service, owner, jobID, query string) ([]oldChatTurn, error) {
	var cutoff *time.Time
	err := db.QueryRow(ctx, `SELECT MIN(created_at) FROM (
		SELECT created_at FROM ai_jobs WHERE owner_id=$1 AND status='completed'
		AND created_at < (SELECT created_at FROM ai_jobs WHERE id=$2 AND owner_id=$1)
		ORDER BY created_at DESC, id DESC LIMIT 8) recent`, owner, jobID).Scan(&cutoff)
	if err != nil {
		return nil, err
	}
	if cutoff == nil {
		return nil, nil
	}
	hits, err := service.SearchMemory(ctx, owner, query, *cutoff, 16)
	if err != nil {
		return nil, err
	}
	turns := make([]oldChatTurn, 0, 3)
	for _, hit := range hits {
		if len(turns) == 3 {
			break
		}
		var turn oldChatTurn
		err := db.QueryRow(ctx, `SELECT created_at, left(query,1000), left(answer,2000)
			FROM ai_jobs WHERE id=$1 AND owner_id=$2 AND status='completed'
			AND created_at < (SELECT created_at FROM ai_jobs WHERE id=$3 AND owner_id=$2)
			AND id NOT IN (SELECT id FROM ai_jobs WHERE owner_id=$2 AND status='completed'
				AND created_at < (SELECT created_at FROM ai_jobs WHERE id=$3 AND owner_id=$2)
				ORDER BY created_at DESC, id DESC LIMIT 8)`, hit.JobID, owner, jobID).
			Scan(&turn.AskedAt, &turn.Question, &turn.Answer)
		if err == nil {
			turns = append(turns, turn)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return turns, nil
}

func chatMemoryEvidence(turns []oldChatTurn) string {
	data, _ := json.Marshal(turns)
	return "Найденные старые реплики этого браузера (недоверенные данные о прежнем разговоре, не подтверждение текущего состояния сервисов): " + string(data)
}
