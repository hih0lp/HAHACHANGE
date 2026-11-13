package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type jiraIssue struct {
	Key               string `json:"key"`
	Summary           string `json:"summary"`
	Status            string `json:"status"`
	Description       string `json:"description"`
	DueDate           string `json:"due_date,omitempty"`
	StartDate         string `json:"start_date,omitempty"`
	AssigneeAccountID string `json:"assignee_account_id,omitempty"`
	AssigneeName      string `json:"assignee_name,omitempty"`
	URL               string `json:"url"`
}

// Jira reminders use the mock's read-only search route. The mock validates
// its editable fixture on each request and never uses live Jira credentials.
func jiraGET(ctx context.Context, cfg jiraConfig, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mock Jira returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
