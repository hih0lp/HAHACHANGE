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
	"time"

	"github.com/hackchange/avito-rag/internal/mockfixture"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type issue struct {
	Key               string         `json:"key"`
	Summary           string         `json:"summary"`
	Status            string         `json:"status"`
	StatusCategory    string         `json:"status_category"`
	Description       string         `json:"description"`
	DueDate           string         `json:"due_date,omitempty"`
	DueAt             string         `json:"due_at,omitempty"`
	StartDate         string         `json:"start_date,omitempty"`
	AssigneeAccountID string         `json:"assignee_account_id,omitempty"`
	AssigneeName      string         `json:"assignee_name,omitempty"`
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
type jiraAccountKey struct{}

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

func load(path string) (fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fixture{}, err
	}
	return parseFixture(data)
}

func parseFixture(data []byte) (fixture, error) {
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
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
		for _, date := range []string{v.DueDate, v.StartDate} {
			if date != "" {
				parsed, parseErr := time.Parse("2006-01-02", date)
				if parseErr != nil || parsed.Format("2006-01-02") != date {
					return f, fmt.Errorf("invalid date in issue %q", v.Key)
				}
			}
		}
		if v.DueAt != "" {
			if _, err := time.Parse(time.RFC3339, v.DueAt); err != nil {
				return f, fmt.Errorf("invalid due_at in issue %q", v.Key)
			}
		}
		seen[v.Key] = true
	}
	return f, nil
}
func summary(v issue) map[string]any {
	m := map[string]any{"key": v.Key, "summary": v.Summary, "status": v.Status}
	for k, x := range map[string]string{"due_date": v.DueDate, "due_at": v.DueAt, "assignee_account_id": v.AssigneeAccountID, "assignee_name": v.AssigneeName} {
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
	if v.DueAt != "" {
		fields["Точное время окончания (due_at)"] = v.DueAt
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := mockfixture.OpenDatabase(ctx, os.Getenv("MOCK_DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	if db != nil {
		defer db.Close()
	}
	data, err := mockfixture.Seed(ctx, db, "jira", func() ([]byte, error) { return os.ReadFile(path) }, validateJiraFixture)
	if err != nil {
		log.Fatal(err)
	}
	cancel()
	mux := newMockJiraHandler(path, scenario.Client{BaseURL: os.Getenv("TEAM_DIRECTORY_URL")}, data)
	listen := os.Getenv("MOCK_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	log.Fatal(http.ListenAndServe(listen, mux))
}

func validateJiraFixture(data []byte) error {
	f, err := parseFixture(data)
	if err == nil && f.Issues == nil {
		return errors.New("Требуется список задач Jira.")
	}
	return err
}

func newMockJiraHandler(path string, team scenario.Client, datasets ...*mockfixture.Dataset) http.Handler {
	var dataset *mockfixture.Dataset
	if len(datasets) > 0 {
		dataset = datasets[0]
	}
	read := func(ctx context.Context) (fixture, error) {
		if dataset == nil {
			return load(path)
		}
		data, err := dataset.Bytes(ctx)
		if err != nil {
			return fixture{}, err
		}
		return parseFixture(data)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "bobby-mock-jira", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "jira_list_issues", Description: "List issues assigned to the signed-in user in the configured project."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArg) (*mcp.CallToolResult, any, error) {
		f, e := read(ctx)
		if e != nil {
			return nil, nil, e
		}
		account, _ := ctx.Value(jiraAccountKey{}).(string)
		return nil, list(f, account, 10), nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_list_assigned_issues", Description: "Find issues assigned to the signed-in user."}, func(ctx context.Context, _ *mcp.CallToolRequest, a assigneeArg) (*mcp.CallToolResult, any, error) {
		f, e := read(ctx)
		if e != nil {
			return nil, nil, e
		}
		account, _ := ctx.Value(jiraAccountKey{}).(string)
		if a.AccountID != "" && a.AccountID != account {
			return nil, nil, errors.New("account_id does not match signed-in user")
		}
		return nil, list(f, account, 50), nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue", Description: "Fetch an assigned issue's title, status, description, dates, assignee and URL."}, func(ctx context.Context, _ *mcp.CallToolRequest, a keyArg) (*mcp.CallToolResult, any, error) {
		f, e := read(ctx)
		if e != nil {
			return nil, nil, e
		}
		v, e := find(f, a.Key)
		if e != nil {
			return nil, nil, e
		}
		if account, _ := ctx.Value(jiraAccountKey{}).(string); v.AssigneeAccountID != account {
			return nil, nil, errors.New("issue is not assigned to signed-in user")
		}
		return nil, v, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "jira_get_issue_fields", Description: "Fetch an assigned issue's nonempty fields."}, func(ctx context.Context, _ *mcp.CallToolRequest, a keyArg) (*mcp.CallToolResult, any, error) {
		f, e := read(ctx)
		if e != nil {
			return nil, nil, e
		}
		v, e := find(f, a.Key)
		if e != nil {
			return nil, nil, e
		}
		if account, _ := ctx.Value(jiraAccountKey{}).(string); v.AssigneeAccountID != account {
			return nil, nil, errors.New("issue is not assigned to signed-in user")
		}
		return nil, map[string]any{"key": v.Key, "fields": issueFields(v)}, nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if _, err := read(r.Context()); err != nil {
			http.Error(w, "Jira data unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if dataset != nil {
		dataset.Register(mux, team)
		dataset.RegisterMutation(mux, team, "/v1/mock-data/jira/issues/{key}", updateIssue)
	} else {
		mockfixture.Register(mux, path, "jira_save_fixture", os.Getenv("MOCK_FIXTURE_WRITE_TOKEN"), validateJiraFixture)
	}
	mux.HandleFunc("GET /v1/mock-data/jira/issues/{key}", func(w http.ResponseWriter, r *http.Request) {
		readIssue(w, r, read, team)
	})
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "Bearer token required", http.StatusUnauthorized)
			return
		}
		profile, err := team.Profile(r.Context(), token)
		if err != nil || profile.JiraAccountID == "" {
			http.Error(w, "Invalid identity or Jira account", http.StatusUnauthorized)
			return
		}
		stream.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), jiraAccountKey{}, profile.JiraAccountID)))
	})
	mux.HandleFunc("GET /rest/api/3/issue/{key}", func(w http.ResponseWriter, r *http.Request) {
		f, e := read(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		v, e := find(f, r.PathValue("key"))
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		fields := map[string]any{"summary": v.Summary, "status": map[string]any{"name": v.Status, "statusCategory": map[string]string{"key": v.StatusCategory}}, "description": v.Description, "duedate": v.DueDate, "due_at": v.DueAt, "customfield_10015": v.StartDate, "assignee": map[string]string{"accountId": v.AssigneeAccountID, "displayName": v.AssigneeName}}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("expand") == "names" {
			json.NewEncoder(w).Encode(map[string]any{"key": v.Key, "fields": fields, "names": map[string]string{"summary": "Название", "status": "Статус", "description": "Описание", "duedate": "Срок исполнения", "customfield_10015": "Дата начала", "assignee": "Исполнитель"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"key": v.Key, "fields": fields})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		f, e := read(r.Context())
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
			items = append(items, map[string]any{"key": v.Key, "fields": map[string]any{"summary": v.Summary, "duedate": v.DueDate, "due_at": v.DueAt, "assignee": map[string]string{"accountId": v.AssigneeAccountID, "displayName": v.AssigneeName}, "status": map[string]any{"name": v.Status, "statusCategory": map[string]string{"key": v.StatusCategory}}}})
		}
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].(map[string]any)["key"].(string) < items[j].(map[string]any)["key"].(string)
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"issues": items})
	})
	return mux
}
