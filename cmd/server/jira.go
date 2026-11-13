package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type jiraConfig struct{ Mode, BaseURL, ProjectKey, IssueType, Email, APIToken, DataPath string }

func (c jiraConfig) validate() error {
	if c.Mode != "off" && c.Mode != "live" && c.Mode != "mock" {
		return errors.New("JIRA_MODE must be off, live or mock")
	}
	if c.Mode == "off" {
		return nil
	}
	u, err := url.Parse(c.BaseURL)
	if c.Mode == "mock" {
		if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || c.ProjectKey == "" {
			return errors.New("mock Jira needs an internal HTTP origin and project key")
		}
		return nil
	}
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("JIRA_BASE_URL must be an HTTPS site origin")
	}
	if c.ProjectKey == "" || c.IssueType == "" || c.Email == "" || c.APIToken == "" {
		return errors.New("live Jira needs project key, issue type, email and API token")
	}
	return nil
}

type issueInput struct{ Resource, Reason string }
type issueResult struct{ Key, Status, URL string }
type issueTracker interface {
	CreateIssue(context.Context, issueInput) (issueResult, error)
}

type jiraTracker struct {
	config jiraConfig
	client *http.Client
}

func (j jiraTracker) CreateIssue(ctx context.Context, input issueInput) (issueResult, error) {
	payload := map[string]any{"fields": map[string]any{
		"project":     map[string]string{"key": j.config.ProjectKey},
		"issuetype":   map[string]string{"name": j.config.IssueType},
		"summary":     "Hack&Change: тестовая заявка на доступ к " + input.Resource,
		"description": map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Демонстрационный запрос стажёра. Ресурс: " + input.Resource + ". Причина: " + input.Reason + ". Не является заявкой Авито."}}}}},
		"labels":      []string{"hackchange-demo"},
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return issueResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.config.BaseURL+"/rest/api/3/issue", bytes.NewReader(body))
	if err != nil {
		return issueResult{}, err
	}
	req.SetBasicAuth(j.config.Email, j.config.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := j.client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return issueResult{}, fmt.Errorf("Jira request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return issueResult{}, err
	}
	if resp.StatusCode != http.StatusCreated {
		return issueResult{}, fmt.Errorf("Jira returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var created struct {
		Key string `json:"key"`
	}
	if err = json.Unmarshal(data, &created); err != nil || created.Key == "" {
		return issueResult{}, errors.New("Jira did not return issue key")
	}
	result := issueResult{Key: created.Key, URL: j.config.BaseURL + "/browse/" + url.PathEscape(created.Key), Status: "Created"}
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, j.config.BaseURL+"/rest/api/3/issue/"+url.PathEscape(created.Key)+"?fields=status", nil)
	if err != nil {
		return result, nil
	}
	getReq.SetBasicAuth(j.config.Email, j.config.APIToken)
	getReq.Header.Set("Accept", "application/json")
	got, err := client.Do(getReq)
	if err != nil {
		return result, nil
	}
	defer got.Body.Close()
	if got.StatusCode == http.StatusOK {
		var detail struct {
			Fields struct {
				Status struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		}
		if json.NewDecoder(io.LimitReader(got.Body, 4096)).Decode(&detail) == nil && detail.Fields.Status.Name != "" {
			result.Status = detail.Fields.Status.Name
		}
	}
	return result, nil
}
