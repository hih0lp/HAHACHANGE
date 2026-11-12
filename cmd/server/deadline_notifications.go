package main

import (
	"context"
	"net/http"
	"strings"
	"time"

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

func mentorWindow(now, due time.Time) bool {
	return !now.Before(due.Add(-24*time.Hour)) && now.Before(due)
}

func scanDeadlineNotifications(ctx context.Context, db *pgxpool.Pool, jira jiraConfig, cfg reminderConfig, directory scenario.Client, now time.Time) error {
	if jira.Mode != "live" || cfg.InternAccountID == "" {
		return nil
	}
	login, err := directory.Login(ctx, "intern", env("TEAM_ACCESS_INTERN_PASSWORD", ""))
	if err != nil {
		return err
	}
	profile, err := directory.Profile(ctx, login.AccessToken)
	if err != nil {
		return err
	}
	if profile.JiraAccountID != "" && !strings.HasPrefix(profile.JiraAccountID, "demo-") {
		cfg.InternAccountID = profile.JiraAccountID
	}
	mentor := ""
	for _, team := range profile.Teams {
		if team.ID == profile.CurrentTeamID {
			mentor = team.MentorID
			break
		}
	}
	cfg.HoursBefore = 48
	snapshot, err := loadReminders(ctx, jira, cfg, now)
	if err != nil {
		return err
	}
	for _, item := range snapshot.Reminders {
		kind, recipient := "intern_48h", profile.ID
		if _, err = db.Exec(ctx, `INSERT INTO ai_deadline_notifications(recipient_id,kind,issue_key,summary,due_at,issue_url) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, recipient, kind, item.Key, item.Summary, item.DueAt, item.URL); err != nil {
			return err
		}
		if mentor != "" && mentorWindow(now, item.DueAt) {
			kind, recipient = "mentor_24h", mentor
			if _, err = db.Exec(ctx, `INSERT INTO ai_deadline_notifications(recipient_id,kind,issue_key,summary,due_at,issue_url) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, recipient, kind, item.Key, item.Summary, item.DueAt, item.URL); err != nil {
				return err
			}
		}
	}
	return nil
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
		rows, err := db.Query(r.Context(), `SELECT id,kind,issue_key,summary,due_at,issue_url,created_at FROM ai_deadline_notifications WHERE recipient_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, recipient)
		if err != nil {
			writeError(w, 503, errText("notifications unavailable"))
			return
		}
		defer rows.Close()
		items := []deadlineNotification{}
		for rows.Next() {
			var item deadlineNotification
			if rows.Scan(&item.ID, &item.Kind, &item.Key, &item.Summary, &item.DueAt, &item.URL, &item.CreatedAt) != nil {
				writeError(w, 503, errText("notifications unavailable"))
				return
			}
			items = append(items, item)
		}
		if rows.Err() != nil {
			writeError(w, 503, errText("notifications unavailable"))
			return
		}
		writeJSON(w, 200, map[string]any{"notifications": items})
	})))
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			_ = scanDeadlineNotifications(ctx, db, jira, cfg, directory, time.Now())
			cancel()
			<-ticker.C
		}
	}()
}
