package main

import (
	"net/http"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/jackc/pgx/v5/pgtype"
)

type chatThread struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func validThreadID(id string) bool {
	var uuid pgtype.UUID
	return uuid.Scan(id) == nil && uuid.Valid
}

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
	mux.Handle("GET /v1/chat/threads", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := ownerFrom(r)
		if owner == "" {
			writeError(w, http.StatusUnauthorized, errText("chat session is required"))
			return
		}
		rows, err := jobs.DB.Query(r.Context(), `SELECT id, title FROM (
			SELECT id::text, title, updated_at FROM ai_chat_threads WHERE owner_id=$1
			UNION ALL
			SELECT 'legacy', 'Прежняя переписка', max(created_at) FROM ai_jobs
			WHERE owner_id=$1 AND thread_id IS NULL HAVING count(*) > 0
		) AS threads ORDER BY updated_at DESC, id DESC`, owner)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, errText("chat list is unavailable"))
			return
		}
		defer rows.Close()
		threads := make([]chatThread, 0)
		for rows.Next() {
			var thread chatThread
			if err := rows.Scan(&thread.ID, &thread.Title); err != nil {
				writeError(w, http.StatusServiceUnavailable, errText("chat list is unavailable"))
				return
			}
			threads = append(threads, thread)
		}
		if rows.Err() != nil {
			writeError(w, http.StatusServiceUnavailable, errText("chat list is unavailable"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
	})))

	mux.Handle("GET /v1/chat/history", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := ownerFrom(r)
		if owner == "" {
			writeError(w, http.StatusUnauthorized, errText("chat session is required"))
			return
		}
		thread := r.URL.Query().Get("thread_id")
		switch thread {
		case "", "legacy":
			thread = ""
		default:
			if !validThreadID(thread) {
				writeError(w, http.StatusBadRequest, errText("invalid chat ID"))
				return
			}
			var exists bool
			err := jobs.DB.QueryRow(r.Context(), `SELECT EXISTS(
				SELECT 1 FROM ai_chat_threads WHERE id=$1 AND owner_id=$2)`, thread, owner).Scan(&exists)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, errText("chat history is unavailable"))
				return
			}
			if !exists {
				writeError(w, http.StatusNotFound, errText("chat not found"))
				return
			}
		}
		query := `SELECT id::text, query, status, COALESCE(answer,''), COALESCE(error,''), created_at, completed_at FROM ai_jobs WHERE owner_id=$1`
		order := ` ORDER BY created_at DESC, id DESC LIMIT 100`
		args := []any{owner}
		if thread == "" {
			query += ` AND thread_id IS NULL`
		} else {
			query += ` AND thread_id=$2`
			args = append(args, thread)
		}
		rows, err := jobs.DB.Query(r.Context(), query+order, args...)
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
