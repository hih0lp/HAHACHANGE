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
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
	StartDate   string `json:"start_date,omitempty"`
	URL         string `json:"url"`
}
type jiraIssues struct {
	Issues []jiraIssue `json:"issues"`
}
type jiraIssueArg struct {
	Key string `json:"key" jsonschema:"Jira issue key, for example SCRUM-5"`
}
type jiraEmptyArg struct{}

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
				Name string `json:"name"`
			} `json:"status"`
			Description any    `json:"description"`
			DueDate     string `json:"duedate"`
			StartDate   string `json:"customfield_10015"`
		} `json:"fields"`
	}
	err := jiraGET(ctx, cfg, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields=summary,status,description,duedate,customfield_10015", &raw)
	if err != nil {
		return jiraIssue{}, err
	}
	return jiraIssue{Key: raw.Key, Summary: raw.Fields.Summary, Status: raw.Fields.Status.Name, Description: adfText(raw.Fields.Description), DueDate: raw.Fields.DueDate, StartDate: raw.Fields.StartDate, URL: cfg.BaseURL + "/browse/" + url.PathEscape(raw.Key)}, nil
}
func jiraListIssues(ctx context.Context, cfg jiraConfig) (jiraIssues, error) {
	params := url.Values{"jql": {"project = " + cfg.ProjectKey + " ORDER BY updated DESC"}, "maxResults": {"10"}, "fields": {"summary,status"}}
	var raw struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := jiraGET(ctx, cfg, "/rest/api/3/search/jql?"+params.Encode(), &raw); err != nil {
		return jiraIssues{}, err
	}
	result := jiraIssues{Issues: make([]jiraIssue, 0, len(raw.Issues))}
	for _, item := range raw.Issues {
		result.Issues = append(result.Issues, jiraIssue{Key: item.Key, Summary: item.Fields.Summary, Status: item.Fields.Status.Name, URL: cfg.BaseURL + "/browse/" + url.PathEscape(item.Key)})
	}
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
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue", Description: "Read a task from the configured test Jira project, including its description, status and URL."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg jiraIssueArg) (*mcp.CallToolResult, jiraIssue, error) {
		if cfg.Mode != "live" {
			return nil, jiraIssue{}, errors.New("Jira is disabled")
		}
		issue, err := jiraGetIssue(ctx, cfg, arg.Key)
		return nil, issue, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_list_issues", Description: "List up to ten recently updated tasks in the configured test Jira project."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ jiraEmptyArg) (*mcp.CallToolResult, jiraIssues, error) {
		if cfg.Mode != "live" {
			return nil, jiraIssues{}, errors.New("Jira is disabled")
		}
		issues, err := jiraListIssues(ctx, cfg)
		return nil, issues, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue_fields", Description: "Read all nonempty Jira fields and their display names for a task when the user asks for details not present in the short issue view."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg jiraIssueArg) (*mcp.CallToolResult, map[string]any, error) {
		if cfg.Mode != "live" {
			return nil, nil, errors.New("Jira is disabled")
		}
		result, err := jiraGetAllFields(ctx, cfg, arg.Key)
		return nil, result, err
	})
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
