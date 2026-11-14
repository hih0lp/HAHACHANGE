package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

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

func formatMockJiraList(result any) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	var response struct {
		ProjectKey string `json:"project_key"`
		Issues     []struct {
			Key     string `json:"key"`
			Summary string `json:"summary"`
			Status  string `json:"status"`
			DueDate string `json:"due_date"`
			URL     string `json:"url"`
		} `json:"issues"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil || response.ProjectKey == "" || envelope["issues"] == nil {
		return "", errors.New("incomplete Jira list")
	}
	if len(response.Issues) == 0 {
		return "В проекте " + response.ProjectKey + " нет задач, назначенных вам.", nil
	}
	lines := []string{"Задачи, назначенные вам в проекте " + response.ProjectKey + ":"}
	for _, issue := range response.Issues {
		if issue.Key == "" || issue.Summary == "" || issue.Status == "" {
			return "", errors.New("incomplete Jira issue in list")
		}
		line := "- **" + issue.Key + "** — " + issue.Summary + "; статус: " + issue.Status
		if issue.DueDate != "" {
			line += "; срок: " + issue.DueDate
		}
		if strings.HasPrefix(issue.URL, "https://") {
			line += "; [открыть задачу](" + issue.URL + ")"
		}
		lines = append(lines, line)
	}
	if response.Truncated {
		lines = append(lines, "Показана только первая страница задач.")
	}
	return strings.Join(lines, "\n"), nil
}
