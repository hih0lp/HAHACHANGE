package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/jackc/pgx/v5"
)

func registerDistributedAccessRoutes(mux *http.ServeMux, key string, cfg jiraConfig, system *distributed.System) {
	read := func(ctx context.Context, owner, id string) (accessRequest, string, error) {
		var result accessRequest
		var state string
		err := system.DB.QueryRow(ctx, `SELECT id::text, resource, reason, COALESCE(external_key,''),
			COALESCE(external_status,''), COALESCE(external_url,''), mode, created_at, status, idempotency_key
			FROM ai_actions WHERE id=$1 AND owner_id=$2`, id, owner).Scan(
			&result.ID, &result.Resource, &result.Reason, &result.Key, &result.Status,
			&result.URL, &result.Mode, &result.CreatedAt, &state, &result.IdempotencyKey)
		result.NextStep = "Откройте задачу и дождитесь ответа ответственного за доступ."
		if result.Status == "" {
			result.Status = state
		}
		return result, state, err
	}
	mux.Handle("POST /v1/access-requests", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Resource, Reason, Role string
			Confirmed              bool
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil {
			writeError(w, 400, err)
			return
		}
		input.Resource, input.Reason = strings.TrimSpace(input.Resource), strings.TrimSpace(input.Reason)
		if !input.Confirmed || len([]rune(input.Resource)) < 2 || len([]rune(input.Resource)) > 120 || len([]rune(input.Reason)) < 2 || len([]rune(input.Reason)) > 500 {
			writeError(w, 400, errText("confirmation, resource and reason are required"))
			return
		}
		idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if len(idem) < 8 || len(idem) > 128 {
			writeError(w, 400, errText("Idempotency-Key must be 8–128 characters"))
			return
		}
		owner := ownerFrom(r)
		id, err := distributed.NewID()
		if err != nil {
			writeError(w, 500, err)
			return
		}
		var selectedID string
		err = system.DB.QueryRow(r.Context(), `INSERT INTO ai_actions(id,owner_id,request_id,idempotency_key,kind,resource,reason,mode,status)
			SELECT $1,$2,j.id,$4,'jira_access',$5,$6,$7,'pending' FROM ai_jobs AS j
			WHERE j.id=$3 AND j.owner_id=$2 AND j.status='completed'
			ON CONFLICT(owner_id,idempotency_key) DO UPDATE SET updated_at=ai_actions.updated_at
			RETURNING id::text`, id, owner, r.Header.Get("X-Request-ID"), idem, input.Resource, input.Reason, cfg.Mode).Scan(&selectedID)
		if err != nil {
			// An action must be linked to a completed question. The caller receives
			// a clear error instead of silently creating an untracked Jira issue.
			writeError(w, 400, errText("a valid request ID is required for this action"))
			return
		}
		record, state, err := read(r.Context(), owner, selectedID)
		if err != nil {
			writeError(w, 503, errText("action storage is unavailable"))
			return
		}
		if record.Resource != input.Resource || record.Reason != input.Reason {
			writeError(w, 409, errText("idempotency key was used for different input"))
			return
		}
		if state == "completed" {
			writeJSON(w, 200, record)
			return
		}
		if state != "pending" {
			writeError(w, 409, errText("action is in progress or needs reconciliation"))
			return
		}
		var claimed string
		err = system.DB.QueryRow(r.Context(), `UPDATE ai_actions SET status='running', updated_at=now()
			WHERE id=$1 AND owner_id=$2 AND status='pending' RETURNING id::text`, selectedID, owner).Scan(&claimed)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 409, errText("action is already running"))
			return
		}
		if err != nil {
			writeError(w, 503, errText("action storage is unavailable"))
			return
		}
		tracker := jiraTracker{config: cfg}
		// The reference is included in the test issue so an uncertain outcome
		// can be reconciled manually without sending a duplicate POST.
		issue, err := tracker.CreateIssue(r.Context(), issueInput{Resource: input.Resource, Reason: input.Reason + "\nAI-buddy request: " + selectedID})
		if err != nil {
			_, _ = system.DB.Exec(context.Background(), `UPDATE ai_actions SET status='uncertain', updated_at=now() WHERE id=$1 AND status='running'`, selectedID)
			writeError(w, 502, errText("Jira outcome is uncertain; check the test project before retrying"))
			return
		}
		_, err = system.DB.Exec(r.Context(), `UPDATE ai_actions SET status='completed', external_key=$2,
			external_url=$3, external_status=$4, updated_at=now() WHERE id=$1 AND status='running'`, selectedID, issue.Key, issue.URL, issue.Status)
		if err != nil {
			writeError(w, 503, errText("Jira may have created the issue; check its reference before retrying"))
			return
		}
		record, _, err = read(r.Context(), owner, selectedID)
		if err != nil {
			writeError(w, 503, errText("action storage is unavailable"))
			return
		}
		writeJSON(w, 201, record)
	})))
	mux.Handle("GET /v1/access-requests/{id}", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record, _, err := read(r.Context(), ownerFrom(r), r.PathValue("id"))
		if err != nil {
			writeError(w, 404, errText("request not found"))
			return
		}
		writeJSON(w, 200, record)
	})))
	mux.Handle("GET /v1/access-requests", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows, err := system.DB.Query(r.Context(), `SELECT id::text FROM ai_actions WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 50`, ownerFrom(r))
		if err != nil {
			writeError(w, 503, errText("action storage is unavailable"))
			return
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err != nil {
			writeError(w, 503, errText("action storage is unavailable"))
			return
		}
		items := make([]accessRequest, 0, len(ids))
		for _, id := range ids {
			record, _, err := read(r.Context(), ownerFrom(r), id)
			if err == nil {
				items = append(items, record)
			}
		}
		writeJSON(w, 200, map[string]any{"requests": items})
	})))
}
