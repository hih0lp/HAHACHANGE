package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type issue struct {
	Key               string         `json:"key"`
	Summary           string         `json:"summary"`
	Status            string         `json:"status"`
	StatusCategory    string         `json:"status_category"`
	Description       string         `json:"description"`
	DueDate           string         `json:"due_date,omitempty"`
	StartDate         string         `json:"start_date,omitempty"`
	AssigneeAccountID string         `json:"assignee_account_id,omitempty"`
	AssigneeName      string         `json:"assignee_name,omitempty"`
	URL               string         `json:"url"`
	Fields            map[string]any `json:"fields,omitempty"`
}
type fixture struct {
	ProjectKey string  `json:"project_key"`
	Issues     []issue `json:"issues"`
}
type keyArg struct {
	Key string `json:"key"`
}
type assigneeArg struct {
	AccountID string `json:"account_id"`
}
type emptyArg struct{}

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

func load(path string) (fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fixture{}, err
	}
	var f fixture
	if err = json.Unmarshal(data, &f); err != nil {
		return f, err
	}
	if f.ProjectKey == "" {
		return f, errors.New("project_key is required")
	}
	seen := map[string]bool{}
	for _, v := range f.Issues {
		if !keyPattern.MatchString(v.Key) || !strings.HasPrefix(v.Key, f.ProjectKey+"-") || v.Summary == "" || v.Status == "" || seen[v.Key] {
			return f, fmt.Errorf("invalid or duplicate issue %q", v.Key)
		}
		seen[v.Key] = true
	}
	return f, nil
}
func summary(v issue) map[string]any {
	m := map[string]any{"key": v.Key, "summary": v.Summary, "status": v.Status, "url": v.URL}
	for k, x := range map[string]string{"due_date": v.DueDate, "assignee_account_id": v.AssigneeAccountID, "assignee_name": v.AssigneeName} {
		if x != "" {
			m[k] = x
		}
	}
	return m
}
func list(f fixture, account string, limit int) map[string]any {
	items := []any{}
	for _, v := range f.Issues {
		if account != "" && v.AssigneeAccountID != account {
			continue
		}
		items = append(items, summary(v))
	}
	truncated := len(items) > limit
	if truncated {
		items = items[:limit]
	}
	return map[string]any{"project_key": f.ProjectKey, "issues": items, "truncated": truncated}
}
func find(f fixture, key string) (issue, error) {
	for _, v := range f.Issues {
		if v.Key == key {
			return v, nil
		}
	}
	return issue{}, fmt.Errorf("issue %s not found", key)
}
func issueFields(v issue) map[string]any {
	fields := map[string]any{"Название (summary)": v.Summary, "Статус (status)": map[string]any{"name": v.Status}, "Описание (description)": v.Description}
	if v.DueDate != "" {
		fields["Срок исполнения (duedate)"] = v.DueDate
	}
	if v.StartDate != "" {
		fields["Дата начала (customfield_10015)"] = v.StartDate
	}
	if v.AssigneeAccountID != "" {
		fields["Исполнитель (assignee)"] = map[string]string{"accountId": v.AssigneeAccountID, "displayName": v.AssigneeName}
	}
	for k, x := range v.Fields {
		if x != nil {
			fields[k] = x
		}
	}
	return fields
}
func main() {
	path := os.Getenv("MOCK_JIRA_JSON")
	if path == "" {
		log.Fatal("MOCK_JIRA_JSON is required")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "bobby-mock-jira", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "jira_list_issues", Description: "Discover recently updated issues in the configured project; fetch detail for description and start date."}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyArg) (*mcp.CallToolResult, any, error) {
		f, e := load(path)
		if e != nil {
			return nil, nil, e
		}
		return nil, list(f, "", 10), nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_list_assigned_issues", Description: "Find issues assigned to a verified Jira accountId in the configured project."}, func(_ context.Context, _ *mcp.CallToolRequest, a assigneeArg) (*mcp.CallToolResult, any, error) {
		f, e := load(path)
		if e != nil {
			return nil, nil, e
		}
		if a.AccountID == "" {
			return nil, nil, errors.New("account_id is required")
		}
		return nil, list(f, a.AccountID, 50), nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue", Description: "Fetch authoritative issue title, status, description, due date, start date, assignee and URL."}, func(_ context.Context, _ *mcp.CallToolRequest, a keyArg) (*mcp.CallToolResult, any, error) {
		f, e := load(path)
		if e != nil {
			return nil, nil, e
		}
		v, e := find(f, a.Key)
		if e != nil {
			return nil, nil, e
		}
		return nil, v, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue_fields", Description: "Fetch nonempty Jira fields with display names and IDs."}, func(_ context.Context, _ *mcp.CallToolRequest, a keyArg) (*mcp.CallToolResult, any, error) {
		f, e := load(path)
		if e != nil {
			return nil, nil, e
		}
		v, e := find(f, a.Key)
		if e != nil {
			return nil, nil, e
		}
		return nil, map[string]any{"key": v.Key, "url": v.URL, "fields": issueFields(v)}, nil
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	mux.HandleFunc("GET /rest/api/3/issue/{key}", func(w http.ResponseWriter, r *http.Request) {
		f, e := load(path)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		v, e := find(f, r.PathValue("key"))
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		fields := map[string]any{"summary": v.Summary, "status": map[string]any{"name": v.Status, "statusCategory": map[string]string{"key": v.StatusCategory}}, "description": v.Description, "duedate": v.DueDate, "customfield_10015": v.StartDate, "assignee": map[string]string{"accountId": v.AssigneeAccountID, "displayName": v.AssigneeName}}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("expand") == "names" {
			json.NewEncoder(w).Encode(map[string]any{"key": v.Key, "fields": fields, "names": map[string]string{"summary": "Название", "status": "Статус", "description": "Описание", "duedate": "Срок исполнения", "customfield_10015": "Дата начала", "assignee": "Исполнитель"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"key": v.Key, "fields": fields})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		f, e := load(path)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		q := r.URL.Query().Get("jql")
		account := ""
		if i := strings.Index(q, "assignee = \""); i >= 0 {
			tail := q[i+len("assignee = \""):]
			account = strings.SplitN(tail, "\"", 2)[0]
		}
		items := []any{}
		for _, v := range f.Issues {
			if account != "" && v.AssigneeAccountID != account {
				continue
			}
			items = append(items, map[string]any{"key": v.Key, "fields": map[string]any{"summary": v.Summary, "duedate": v.DueDate, "assignee": map[string]string{"accountId": v.AssigneeAccountID, "displayName": v.AssigneeName}, "status": map[string]any{"name": v.Status, "statusCategory": map[string]string{"key": v.StatusCategory}}}})
		}
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].(map[string]any)["key"].(string) < items[j].(map[string]any)["key"].(string)
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"issues": items})
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}
