package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var jiraKeyMentionPattern = regexp.MustCompile(`[A-Z][A-Z0-9_]*-[0-9]+`)

func formatMockJiraCard(result any) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	var issue jiraIssue
	if err := json.Unmarshal(raw, &issue); err != nil {
		return "", err
	}
	if issue.Key == "" || issue.Summary == "" || issue.Status == "" {
		return "", errors.New("incomplete Jira issue")
	}
	assignee := issue.AssigneeName
	if assignee == "" {
		assignee = "не назначен"
	}
	start, due := issue.StartDate, issue.DueDate
	if start == "" {
		start = "не указана"
	}
	if due == "" {
		due = "не указан"
	}
	description := issue.Description
	if description == "" {
		description = "не заполнено"
	}
	lines := []string{
		fmt.Sprintf("**%s** — %s", issue.Key, issue.Summary),
		"Статус: " + issue.Status,
		"Исполнитель: " + assignee,
		"Дата начала: " + start,
		"Срок: " + due,
		"Описание: " + description,
	}
	if strings.HasPrefix(issue.URL, "https://") {
		lines = append(lines, "[Открыть задачу]("+issue.URL+")")
	}
	return strings.Join(lines, "\n"), nil
}
