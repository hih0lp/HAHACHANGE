package main

import (
	"net/http"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
)

type chatTurn struct {
	ID          string     `json:"id"`
	Query       string     `json:"query"`
	Status      string     `json:"status"`
	Answer      string     `json:"answer,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

func registerHistoryRoutes(mux *http.ServeMux, apiKey string, jobs *distributed.System) {
	mux.Handle("GET /v1/chat/history", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := ownerFrom(r)
		if owner == "" {
			writeError(w, http.StatusUnauthorized, errText("chat session is required"))
			return
		}
		rows, err := jobs.DB.Query(r.Context(), `SELECT id::text, query, status, COALESCE(answer,''), COALESCE(error,''), created_at, completed_at
			FROM ai_jobs WHERE owner_id=$1 ORDER BY created_at DESC, id DESC LIMIT 100`, owner)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, errText("chat history is unavailable"))
			return
		}
		defer rows.Close()
		items := make([]chatTurn, 0)
		for rows.Next() {
			var item chatTurn
			if err := rows.Scan(&item.ID, &item.Query, &item.Status, &item.Answer, &item.Error, &item.CreatedAt, &item.CompletedAt); err != nil {
				writeError(w, http.StatusServiceUnavailable, errText("chat history is unavailable"))
				return
			}
			items = append(items, item)
		}
		if rows.Err() != nil {
			writeError(w, http.StatusServiceUnavailable, errText("chat history is unavailable"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"turns": items})
	})))
}
