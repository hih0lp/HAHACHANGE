package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata" // The minimal runtime image does not contain system timezone files.

	"github.com/gorilla/websocket"
)

type reminderConfig struct {
	InternAccountID     string
	HoursBefore         int
	InternMinutesBefore int
	MentorMinutesBefore int
	TimeZone            string
}

type reminder struct {
	URL     string    `json:"url"`
	ID      string    `json:"id"`
	Key     string    `json:"key"`
	Summary string    `json:"summary"`
	DueAt   time.Time `json:"due_at"`
}

type reminderSnapshot struct {
	Enabled   bool       `json:"enabled"`
	Reminders []reminder `json:"reminders"`
	Message   string     `json:"message,omitempty"`
}

// Jira's due date is a date without a time. A task is due at the end of that
// calendar day in the configured timezone, not at midnight at its start.
func reminderDueAt(date string, zone *time.Location) (time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", date, zone)
	if err != nil {
		return time.Time{}, err
	}
	return day.AddDate(0, 0, 1), nil
}

func loadReminders(ctx context.Context, jira jiraConfig, cfg reminderConfig, now time.Time) (reminderSnapshot, error) {
	return loadRemindersWindow(ctx, jira, cfg, now, time.Duration(cfg.HoursBefore)*time.Hour)
}

func loadRemindersWindow(ctx context.Context, jira jiraConfig, cfg reminderConfig, now time.Time, window time.Duration) (reminderSnapshot, error) {
	result := reminderSnapshot{Reminders: []reminder{}}
	if jira.Mode != "mock" || strings.TrimSpace(cfg.InternAccountID) == "" {
		result.Message = "Напоминания выключены: укажите Jira accountId стажёра и подключите мок Jira."
		return result, nil
	}
	zone, err := time.LoadLocation(cfg.TimeZone)
	if err != nil {
		return result, fmt.Errorf("invalid reminder timezone: %w", err)
	}
	result.Enabled = true
	params := url.Values{"jql": {"project = " + jira.ProjectKey + " AND assignee = \"" + strings.ReplaceAll(cfg.InternAccountID, "\"", "") + "\" AND duedate is not EMPTY ORDER BY duedate ASC"}, "maxResults": {"100"}, "fields": {"summary,status,assignee,duedate"}}
	var raw struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary  string `json:"summary"`
				DueDate  string `json:"duedate"`
				Assignee struct {
					AccountID string `json:"accountId"`
				} `json:"assignee"`
				Status struct {
					Category struct {
						Key string `json:"key"`
					} `json:"statusCategory"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := jiraGET(ctx, jira, "/rest/api/3/search/jql?"+params.Encode(), &raw); err != nil {
		return result, err
	}
	for _, issue := range raw.Issues {
		if issue.Fields.Assignee.AccountID != cfg.InternAccountID || issue.Fields.Status.Category.Key == "done" || issue.Fields.DueDate == "" {
			continue
		}
		due, err := reminderDueAt(issue.Fields.DueDate, zone)
		if err != nil || !deadlineWindow(now, due, window) {
			continue
		}
		result.Reminders = append(result.Reminders, reminder{ID: issue.Key + ":" + issue.Fields.DueDate, Key: issue.Key, Summary: issue.Fields.Summary, DueAt: due})
	}
	return result, nil
}

func deadlineWindow(now, due time.Time, window time.Duration) bool {
	return window > 0 && !now.Before(due.Add(-window)) && now.Before(due)
}

func registerReminderRoutes(mux *http.ServeMux, apiKey string, jira jiraConfig, cfg reminderConfig) {
	read := func(ctx context.Context) (reminderSnapshot, error) {
		return loadReminders(ctx, jira, cfg, time.Now())
	}
	mux.Handle("GET /v1/reminders", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, err := read(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, errText("Не удалось проверить задачи Jira"))
			return
		}
		writeJSON(w, http.StatusOK, result)
	})))
	mux.HandleFunc("GET /v1/reminders/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if apiKey != "" {
			conn.SetReadLimit(2048)
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var auth struct {
				APIKey string `json:"api_key"`
			}
			if err := conn.ReadJSON(&auth); err != nil || subtle.ConstantTimeCompare([]byte(auth.APIKey), []byte(apiKey)) != 1 {
				return
			}
			_ = conn.SetReadDeadline(time.Time{})
		}
		closed := make(chan struct{})
		go func() {
			defer close(closed)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			result, err := read(r.Context())
			if err != nil {
				result = reminderSnapshot{Reminders: []reminder{}, Message: "Не удалось проверить задачи Jira"}
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(result); err != nil {
				return
			}
			select {
			case <-closed:
				return
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})
}
