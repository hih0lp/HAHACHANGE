package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/hackchange/avito-rag/internal/mockfixture"
	"github.com/hackchange/avito-rag/internal/scenario"
)

func readIssue(w http.ResponseWriter, r *http.Request, read func(context.Context) (fixture, error), team scenario.Client) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		writeIssueError(w, http.StatusUnauthorized, "Требуется сессия команды.")
		return
	}
	profile, err := team.Profile(r.Context(), token)
	if err != nil {
		writeIssueError(w, http.StatusForbidden, "Недействительная сессия команды.")
		return
	}
	f, err := read(r.Context())
	if err != nil {
		writeIssueError(w, http.StatusServiceUnavailable, "Задачи Jira временно недоступны.")
		return
	}
	item, err := find(f, r.PathValue("key"))
	if err != nil || profile.ID != "approver" && (profile.JiraAccountID == "" || item.AssigneeAccountID != profile.JiraAccountID) {
		writeIssueError(w, http.StatusNotFound, "Задача не найдена.")
		return
	}
	_ = json.NewEncoder(w).Encode(item)
}

func writeIssueError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func updateIssue(data []byte, r *http.Request, body []byte) ([]byte, error) {
	f, err := parseFixture(data)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(body, &fields); err != nil || len(fields) == 0 {
		return nil, errors.New("Укажите изменяемые поля задачи.")
	}
	for name := range fields {
		switch name {
		case "summary", "description", "due_date", "due_at":
		default:
			return nil, errors.New("Неизвестное поле задачи: " + name)
		}
	}
	for i := range f.Issues {
		if f.Issues[i].Key != r.PathValue("key") {
			continue
		}
		v := &f.Issues[i]
		for name, raw := range fields {
			var value string
			if string(raw) != "null" {
				if err := json.Unmarshal(raw, &value); err != nil {
					return nil, errors.New("Поле " + name + " должно быть строкой или null.")
				}
			}
			switch name {
			case "summary":
				v.Summary = value
			case "description":
				v.Description = value
			case "due_date":
				v.DueDate = value
			case "due_at":
				v.DueAt = value
			}
		}
		return json.Marshal(f)
	}
	return nil, mockfixture.ErrItemNotFound
}
