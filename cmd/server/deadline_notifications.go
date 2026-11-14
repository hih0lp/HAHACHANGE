package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/jackc/pgx/v5/pgxpool"
)

type deadlineNotification struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	DueAt     time.Time `json:"due_at"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

func scanDeadlineNotifications(ctx context.Context, db *pgxpool.Pool, jira jiraConfig, cfg reminderConfig, directory scenario.Client, now time.Time) error {
	if jira.Mode != "mock" || (cfg.InternMinutesBefore <= 0 && cfg.MentorMinutesBefore <= 0) {
		return nil
	}
	var errs []error
	for _, role := range []struct{ user, passwordEnv string }{
		{"intern", "TEAM_ACCESS_INTERN_PASSWORD"},
		{"intern2", "TEAM_ACCESS_INTERN2_PASSWORD"},
	} {
		password := env(role.passwordEnv, "")
		if password == "" {
			continue
		}
		if err := scanInternDeadlineNotifications(ctx, db, jira, cfg, directory, now, role.user, password); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", role.user, err))
		}
	}
	return errors.Join(errs...)
}

func scanInternDeadlineNotifications(ctx context.Context, db *pgxpool.Pool, jira jiraConfig, cfg reminderConfig, directory scenario.Client, now time.Time, user, password string) error {
	login, err := directory.Login(ctx, user, password)
	if err != nil {
		return err
	}
	profile, err := directory.Profile(ctx, login.AccessToken)
	if err != nil {
		return err
	}
	cfg.InternAccountID = reminderAccountID(user, profile.JiraAccountID, cfg.InternAccountID)
	if cfg.InternAccountID == "" {
		return nil
	}
	mentor := ""
	for _, team := range profile.Teams {
		if team.ID == profile.CurrentTeamID {
			mentor = team.MentorID
			break
		}
	}
	window := time.Duration(max(cfg.InternMinutesBefore, cfg.MentorMinutesBefore)) * time.Minute
	snapshot, err := loadRemindersWindow(ctx, jira, cfg, now, window)
	if err != nil {
		return err
	}
	for _, item := range snapshot.Reminders {
		if deadlineWindow(now, item.DueAt, time.Duration(cfg.InternMinutesBefore)*time.Minute) {
			if _, err = db.Exec(ctx, `INSERT INTO ai_deadline_notifications(recipient_id,kind,issue_key,summary,due_at,issue_url) VALUES($1,'intern_deadline',$2,$3,$4,$5) ON CONFLICT DO NOTHING`, profile.ID, item.Key, item.Summary, item.DueAt, item.URL); err != nil {
				return err
			}
		}
		if mentor != "" && deadlineWindow(now, item.DueAt, time.Duration(cfg.MentorMinutesBefore)*time.Minute) {
			if _, err = db.Exec(ctx, `INSERT INTO ai_deadline_notifications(recipient_id,kind,issue_key,summary,due_at,issue_url) VALUES($1,'mentor_deadline',$2,$3,$4,$5) ON CONFLICT DO NOTHING`, mentor, item.Key, item.Summary, item.DueAt, item.URL); err != nil {
				return err
			}
		}
	}
	return nil
}

func readDeadlineNotifications(ctx context.Context, db *pgxpool.Pool, recipient string) ([]deadlineNotification, error) {
	kind := "intern_deadline"
	if recipient == "approver" {
		kind = "mentor_deadline"
	}
	rows, err := db.Query(ctx, `SELECT id,kind,issue_key,summary,due_at,issue_url,created_at FROM ai_deadline_notifications WHERE recipient_id=$1 AND kind=$2 ORDER BY created_at DESC,id DESC LIMIT 100`, recipient, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []deadlineNotification{}
	for rows.Next() {
		var item deadlineNotification
		if err := rows.Scan(&item.ID, &item.Kind, &item.Key, &item.Summary, &item.DueAt, &item.URL, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func registerDeadlineNotifications(mux *http.ServeMux, db *pgxpool.Pool, apiKey string, jira jiraConfig, cfg reminderConfig, directory scenario.Client) {
	mux.Handle("GET /v1/deadline-notifications", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ownerFrom(r) == "" {
			writeError(w, 401, errText("chat session required"))
			return
		}
		recipient := "intern"
		if demoRole(r) == "mentor" {
			recipient = "approver"
		} else if demoRole(r) == "intern2" {
			recipient = "intern2"
		}
		items, err := readDeadlineNotifications(r.Context(), db, recipient)
		if err != nil {
			writeError(w, 503, errText("notifications unavailable"))
			return
		}
		writeJSON(w, 200, map[string]any{"notifications": items})
	})))
	var mu sync.Mutex
	scanned := make(chan struct{})
	mux.HandleFunc("GET /v1/deadline-notifications/ws", func(w http.ResponseWriter, r *http.Request) {
		if ownerFrom(r) == "" {
			writeError(w, http.StatusUnauthorized, errText("chat session required"))
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(4096)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var auth struct {
			APIKey      string `json:"api_key"`
			AccessToken string `json:"access_token"`
		}
		if conn.ReadJSON(&auth) != nil || auth.AccessToken == "" ||
			(apiKey != "" && subtle.ConstantTimeCompare([]byte(auth.APIKey), []byte(apiKey)) != 1) {
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			defer cancel()
			for {
				if _, _, err := conn.NextReader(); err != nil {
					return
				}
			}
		}()
		profile, err := directory.Profile(ctx, auth.AccessToken)
		if err != nil || (profile.ID != "intern" && profile.ID != "intern2" && profile.ID != "approver") {
			return
		}
		for {
			mu.Lock()
			next := scanned
			mu.Unlock()
			items, err := readDeadlineNotifications(ctx, db, profile.ID)
			if err != nil {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if conn.WriteJSON(map[string]any{"notifications": items}) != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-next:
			}
		}
	})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			if err := scanDeadlineNotifications(ctx, db, jira, cfg, directory, time.Now()); err != nil {
				log.Printf("deadline notification scan failed: %v", err)
			}
			cancel()
			mu.Lock()
			close(scanned)
			scanned = make(chan struct{})
			mu.Unlock()
			<-ticker.C
		}
	}()
}
