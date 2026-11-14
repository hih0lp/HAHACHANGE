package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // The minimal runtime image does not contain system timezone files.

	"github.com/gorilla/websocket"
	"github.com/hackchange/avito-rag/internal/scenario"
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

func reminderDeadline(dueAt, dueDate string, zone *time.Location) (time.Time, error) {
	if dueAt != "" {
		return time.Parse(time.RFC3339, dueAt)
	}
	return reminderDueAt(dueDate, zone)
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
	params := url.Values{"jql": {"project = " + jira.ProjectKey + " AND assignee = \"" + strings.ReplaceAll(cfg.InternAccountID, "\"", "") + "\" ORDER BY duedate ASC"}, "maxResults": {"100"}, "fields": {"summary,status,assignee,duedate,due_at"}}
	var raw struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary  string `json:"summary"`
				DueDate  string `json:"duedate"`
				DueAt    string `json:"due_at"`
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
		if issue.Fields.Assignee.AccountID != cfg.InternAccountID || issue.Fields.Status.Category.Key == "done" || (issue.Fields.DueDate == "" && issue.Fields.DueAt == "") {
			continue
		}
		due, err := reminderDeadline(issue.Fields.DueAt, issue.Fields.DueDate, zone)
		if err != nil || !deadlineWindow(now, due, window) {
			continue
		}
		result.Reminders = append(result.Reminders, reminder{ID: issue.Key + ":" + due.Format(time.RFC3339), Key: issue.Key, Summary: issue.Fields.Summary, DueAt: due})
	}
	return result, nil
}

func deadlineWindow(now, due time.Time, window time.Duration) bool {
	return window > 0 && !now.Before(due.Add(-window)) && now.Before(due)
}

func reminderAccountID(user, jiraID, fallback string) string {
	if user == "intern" && fallback != "" && (jiraID == "" || strings.HasPrefix(jiraID, "demo-")) {
		return fallback
	}
	return jiraID
}

func registerJiraTasks(mux *http.ServeMux, apiKey string, jira jiraConfig, cfg reminderConfig, directory scenario.Client) {
	mux.Handle("GET /v1/tasks/jira", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, errText("team session required"))
			return
		}
		ctx := r.Context()
		profile, err := directory.Profile(ctx, token)
		if err != nil {
			scenarioError(w, err)
			return
		}
		accounts := map[string]struct{}{}
		if profile.ID == "intern" || profile.ID == "intern2" {
			accounts[reminderAccountID(profile.ID, profile.JiraAccountID, cfg.InternAccountID)] = struct{}{}
		} else {
			for _, team := range profile.Teams {
				if team.MentorID != profile.ID {
					continue
				}
				members, err := directory.Members(ctx, token, team.ID)
				if err != nil {
					scenarioError(w, err)
					return
				}
				for _, member := range members {
					if member.Role == "intern" {
						accounts[reminderAccountID(member.ID, member.JiraAccountID, cfg.InternAccountID)] = struct{}{}
					}
				}
			}
		}
		delete(accounts, "")
		result := reminderSnapshot{Reminders: []reminder{}}
		now := time.Now()
		for account := range accounts {
			taskConfig := cfg
			taskConfig.InternAccountID = account
			snapshot, err := loadReminders(ctx, jira, taskConfig, now)
			if err != nil {
				writeError(w, http.StatusBadGateway, errText("Не удалось проверить задачи Jira"))
				return
			}
			result.Enabled = snapshot.Enabled
			result.Reminders = append(result.Reminders, snapshot.Reminders...)
		}
		sort.Slice(result.Reminders, func(i, j int) bool {
			a, b := result.Reminders[i], result.Reminders[j]
			if a.DueAt.Equal(b.DueAt) {
				return a.Key < b.Key
			}
			return a.DueAt.Before(b.DueAt)
		})
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})))
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
