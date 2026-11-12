package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type jiraIssue struct {
	Key               string `json:"key"`
	Summary           string `json:"summary"`
	Status            string `json:"status"`
	StatusCategory    string `json:"status_category,omitempty"`
	Description       string `json:"description"`
	DueDate           string `json:"due_date,omitempty"`
	StartDate         string `json:"start_date,omitempty"`
	AssigneeAccountID string `json:"assignee_account_id,omitempty"`
	AssigneeName      string `json:"assignee_name,omitempty"`
	URL               string `json:"url"`
}
type jiraIssues struct {
	ProjectKey string             `json:"project_key"`
	Issues     []jiraIssueSummary `json:"issues"`
	Truncated  bool               `json:"truncated,omitempty"`
}
type jiraIssueSummary struct {
	Key               string `json:"key"`
	Summary           string `json:"summary"`
	Status            string `json:"status"`
	StatusCategory    string `json:"status_category,omitempty"`
	DueDate           string `json:"due_date,omitempty"`
	AssigneeAccountID string `json:"assignee_account_id,omitempty"`
	AssigneeName      string `json:"assignee_name,omitempty"`
	URL               string `json:"url"`
}
type jiraIssueArg struct {
	Key string `json:"key" jsonschema:"Jira issue key, for example SCRUM-5"`
}
type jiraEmptyArg struct{}
type jiraAssigneeArg struct {
	AccountID string `json:"account_id" jsonschema:"Verified Jira accountId of the current user from the team profile"`
}

var jiraAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9:._-]+$`)

var issueKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

func jiraGET(ctx context.Context, cfg jiraConfig, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.Email, cfg.APIToken)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Jira returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
func adfText(value any) string {
	var parts []string
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if t, ok := v["text"].(string); ok {
				parts = append(parts, t)
			}
			if children, ok := v["content"].([]any); ok {
				for _, child := range children {
					walk(child)
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(value)
	return strings.Join(parts, "\n")
}
func jiraGetIssue(ctx context.Context, cfg jiraConfig, key string) (jiraIssue, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	if !issueKeyPattern.MatchString(key) || !strings.HasPrefix(key, cfg.ProjectKey+"-") {
		return jiraIssue{}, errors.New("issue key must belong to the configured project")
	}
	var raw struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name           string `json:"name"`
				StatusCategory struct {
					Name string `json:"name"`
				} `json:"statusCategory"`
			} `json:"status"`
			Description any    `json:"description"`
			DueDate     string `json:"duedate"`
			StartDate   string `json:"customfield_10015"`
			Assignee    struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			} `json:"assignee"`
		} `json:"fields"`
	}
	err := jiraGET(ctx, cfg, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields=summary,status,description,duedate,customfield_10015,assignee", &raw)
	if err != nil {
		return jiraIssue{}, err
	}
	return jiraIssue{Key: raw.Key, Summary: raw.Fields.Summary, Status: raw.Fields.Status.Name, StatusCategory: raw.Fields.Status.StatusCategory.Name, Description: adfText(raw.Fields.Description), DueDate: raw.Fields.DueDate, StartDate: raw.Fields.StartDate, AssigneeAccountID: raw.Fields.Assignee.AccountID, AssigneeName: raw.Fields.Assignee.DisplayName, URL: cfg.BaseURL + "/browse/" + url.PathEscape(raw.Key)}, nil
}
func jiraListIssues(ctx context.Context, cfg jiraConfig) (jiraIssues, error) {
	return jiraSearchIssues(ctx, cfg, "")
}

func jiraSearchIssues(ctx context.Context, cfg jiraConfig, accountID string) (jiraIssues, error) {
	if accountID != "" && !jiraAccountIDPattern.MatchString(accountID) {
		return jiraIssues{}, errors.New("invalid Jira accountId")
	}
	jql := "project = " + cfg.ProjectKey
	if accountID != "" {
		jql += ` AND assignee = "` + accountID + `"`
	}
	jql += " ORDER BY updated DESC"
	maxResults := "10"
	if accountID != "" {
		maxResults = "50"
	}
	params := url.Values{"jql": {jql}, "maxResults": {maxResults}, "fields": {"summary,status,assignee,duedate"}}
	var raw struct {
		NextPageToken string `json:"nextPageToken"`
		Issues        []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name           string `json:"name"`
					StatusCategory struct {
						Name string `json:"name"`
					} `json:"statusCategory"`
				} `json:"status"`
				DueDate  string `json:"duedate"`
				Assignee struct {
					AccountID   string `json:"accountId"`
					DisplayName string `json:"displayName"`
				} `json:"assignee"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := jiraGET(ctx, cfg, "/rest/api/3/search/jql?"+params.Encode(), &raw); err != nil {
		return jiraIssues{}, err
	}
	result := jiraIssues{ProjectKey: cfg.ProjectKey, Issues: make([]jiraIssueSummary, 0, len(raw.Issues))}
	for _, item := range raw.Issues {
		if accountID != "" && item.Fields.Assignee.AccountID != accountID {
			continue
		}
		result.Issues = append(result.Issues, jiraIssueSummary{Key: item.Key, Summary: item.Fields.Summary, Status: item.Fields.Status.Name, StatusCategory: item.Fields.Status.StatusCategory.Name, DueDate: item.Fields.DueDate, AssigneeAccountID: item.Fields.Assignee.AccountID, AssigneeName: item.Fields.Assignee.DisplayName, URL: cfg.BaseURL + "/browse/" + url.PathEscape(item.Key)})
	}
	result.Truncated = raw.NextPageToken != ""
	return result, nil
}

func jiraGetAllFields(ctx context.Context, cfg jiraConfig, key string) (map[string]any, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	if !issueKeyPattern.MatchString(key) || !strings.HasPrefix(key, cfg.ProjectKey+"-") {
		return nil, errors.New("issue key must belong to the configured project")
	}
	var raw struct {
		Key    string                     `json:"key"`
		Names  map[string]string          `json:"names"`
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := jiraGET(ctx, cfg, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields=*all&expand=names", &raw); err != nil {
		return nil, err
	}
	fields := make(map[string]json.RawMessage)
	for id, value := range raw.Fields {
		if string(value) == "null" || len(value) == 0 {
			continue
		}
		name := raw.Names[id]
		if name == "" {
			name = id
		}
		fields[name+" ("+id+")"] = value
	}
	return map[string]any{"key": raw.Key, "url": cfg.BaseURL + "/browse/" + url.PathEscape(raw.Key), "fields": fields}, nil
}
func newJiraMCP(cfg jiraConfig) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "hackchange-jira", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue", Description: "Fetch authoritative details of one Jira issue: title (summary), status, description if present, due_date, start_date and URL. Use this for facts about a specific issue and for each issue in a comparison. A missing date is known only from this issue result, never from status or a list entry."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg jiraIssueArg) (*mcp.CallToolResult, jiraIssue, error) {
		if cfg.Mode != "live" {
			return nil, jiraIssue{}, errors.New("Jira is disabled")
		}
		issue, err := jiraGetIssue(ctx, cfg, arg.Key)
		return nil, issue, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_list_issues", Description: "Discover up to ten recently updated issues in the configured Jira project. Includes assignee accountId, title, status, due date and URL. This is a project-wide list, not a personal task list; compare assignee with the verified team profile or use jira_list_assigned_issues. Fetch each issue separately for descriptions, start dates or other fields."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ jiraEmptyArg) (*mcp.CallToolResult, jiraIssues, error) {
		if cfg.Mode != "live" {
			return nil, jiraIssues{}, errors.New("Jira is disabled")
		}
		issues, err := jiraListIssues(ctx, cfg)
		return nil, issues, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_list_assigned_issues", Description: "Find issues assigned to the current employee in the configured Jira project. Supply their verified Jira accountId from the team profile, never an inferred name or a demo placeholder. Results include due dates and a truncated flag when more than 50 match."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg jiraAssigneeArg) (*mcp.CallToolResult, jiraIssues, error) {
		if cfg.Mode != "live" {
			return nil, jiraIssues{}, errors.New("Jira is disabled")
		}
		if arg.AccountID == "" {
			return nil, jiraIssues{}, errors.New("Jira accountId is required")
		}
		issues, err := jiraSearchIssues(ctx, cfg, arg.AccountID)
		return nil, issues, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue_fields", Description: "Fetch all nonempty fields of one Jira issue with Jira's display names and field IDs. Use when a requested fact is absent from jira_get_issue or a field needs clarification. Omitted null fields are not evidence of any workflow rule."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg jiraIssueArg) (*mcp.CallToolResult, map[string]any, error) {
		if cfg.Mode != "live" {
			return nil, nil, errors.New("Jira is disabled")
		}
		result, err := jiraGetAllFields(ctx, cfg, arg.Key)
		return nil, result, err
	})
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
