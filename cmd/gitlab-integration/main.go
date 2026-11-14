package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type query struct {
	Project string `json:"project" jsonschema:"GitLab numeric project ID or full namespace/project"`
	Ref     string `json:"ref,omitempty" jsonschema:"branch, tag or commit SHA"`
	Path    string `json:"path,omitempty" jsonschema:"repository-relative file or directory"`
	ID      string `json:"id,omitempty" jsonschema:"commit SHA or merge request IID as required by this tool"`
}

type operation struct {
	name, description, suffix string
	requiresID, requiresFile  bool
}

var operations = []operation{
	{"gitlab_projects", "List up to 25 projects visible to the connected GitLab account.", "", false, false},
	{"gitlab_tree", "List repository files and directories at a branch, tag or commit. Returns the first 25 entries.", "/repository/tree", false, false},
	{"gitlab_branches", "List up to 25 repository branches.", "/repository/branches", false, false},
	{"gitlab_tags", "List up to 25 repository tags.", "/repository/tags", false, false},
	{"gitlab_file", "Read a repository file as text at a specified branch, tag or commit.", "/repository/files", false, true},
	{"gitlab_commits", "List the latest 25 commits at a branch, tag or commit, optionally filtered by file path.", "/repository/commits", false, false},
	{"gitlab_commit_diff", "Read changes in a commit. Provide its SHA as id.", "/repository/commits", true, false},
	{"gitlab_merge_requests", "List up to 25 open merge requests and their review status metadata.", "/merge_requests", false, false},
	{"gitlab_review_reminders", "List up to 25 open non-draft merge requests to follow up. The result does not claim required approvals unless GitLab confirms them.", "/merge_requests", false, false},
	{"gitlab_merge_request", "Read a merge request and its current metadata. Provide the project-local MR number as id.", "/merge_requests", true, false},
	{"gitlab_merge_request_notes", "Read up to 25 comments on a merge request. Provide the project-local MR number as id.", "/merge_requests", true, false},
	{"gitlab_merge_request_diff", "Read changes in a merge request. Provide the project-local MR number as id.", "/merge_requests", true, false},
	{"gitlab_approval_state", "Read GitLab approval rules and status for a merge request. Provide the project-local MR number as id.", "/merge_requests", true, false},
	{"gitlab_pipelines", "List up to 25 recent pipelines, optionally filtered by ref.", "/pipelines", false, false},
}

type bridge struct {
	gitlab, directory, subject, token string
	tokens                            map[string]string
	client                            *http.Client
	mock                              bool
}

func (b bridge) authorize(r *http.Request) (string, bool) {
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || bearer == "" {
		return "", false
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, b.directory+"/v1/me", nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var profile struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&profile) != nil {
		return "", false
	}
	if b.mock {
		return profile.ID, profile.ID != ""
	}
	if len(b.tokens) != 0 {
		token := b.tokens[profile.ID]
		return token, token != ""
	}
	return b.token, profile.ID == b.subject && b.token != ""
}

func (b bridge) call(ctx context.Context, op operation, in query) (map[string]any, error) {
	if (op.name != "gitlab_projects" && in.Project == "") || in.Project == "." || in.Project == ".." || in.ID == "." || in.ID == ".." || in.Path == "." || in.Path == ".." || (op.requiresID && in.ID == "") || (op.requiresFile && (in.Path == "" || in.Ref == "")) {
		return nil, errors.New("project and the required tool arguments must be supplied")
	}
	if op.name == "gitlab_review_reminders" {
		return b.reviewReminders(ctx, in.Project)
	}
	path := "/api/v4/projects/" + url.PathEscape(in.Project) + op.suffix
	values := url.Values{"per_page": {"25"}}
	switch op.name {
	case "gitlab_projects":
		path = "/api/v4/projects"
		values.Set("membership", "true")
	case "gitlab_file":
		path += "/" + url.PathEscape(in.Path) + "/raw"
		values.Set("ref", in.Ref)
	case "gitlab_tree":
		if in.Ref != "" {
			values.Set("ref", in.Ref)
		}
		if in.Path != "" {
			values.Set("path", in.Path)
		}
	case "gitlab_commits":
		if in.Ref != "" {
			values.Set("ref_name", in.Ref)
		}
		if in.Path != "" {
			values.Set("path", in.Path)
		}
	case "gitlab_pipelines":
		if in.Ref != "" {
			values.Set("ref", in.Ref)
		}
	case "gitlab_merge_requests":
		values.Set("state", "opened")
	case "gitlab_commit_diff":
		path += "/" + url.PathEscape(in.ID) + "/diff"
	case "gitlab_merge_request_diff":
		path += "/" + url.PathEscape(in.ID) + "/diffs"
	case "gitlab_merge_request":
		path += "/" + url.PathEscape(in.ID)
	case "gitlab_merge_request_notes":
		path += "/" + url.PathEscape(in.ID) + "/notes"
	case "gitlab_approval_state":
		path += "/" + url.PathEscape(in.ID) + "/approval_state"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.gitlab+path+"?"+values.Encode(), nil)
	if err != nil {
		return nil, errors.New("invalid GitLab request")
	}
	req.Header.Set("PRIVATE-TOKEN", b.token)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, errors.New("GitLab is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && op.name == "gitlab_approval_state" {
		fallback, err := http.NewRequestWithContext(ctx, http.MethodGet, b.gitlab+"/api/v4/projects/"+url.PathEscape(in.Project)+"/merge_requests/"+url.PathEscape(in.ID)+"/approvals", nil)
		if err != nil {
			return nil, errors.New("invalid GitLab approval request")
		}
		fallback.Header.Set("PRIVATE-TOKEN", b.token)
		resp.Body.Close()
		resp, err = b.client.Do(fallback)
		if err != nil {
			return nil, errors.New("GitLab approval status is unavailable")
		}
		defer resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab returned HTTP %d", resp.StatusCode)
	}
	const limit = 128 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(body) > limit {
		return nil, errors.New("GitLab response exceeds the supported size or could not be read")
	}
	if op.requiresFile {
		return map[string]any{"project": in.Project, "path": in.Path, "ref": in.Ref, "content": string(body)}, nil
	}
	if !json.Valid(body) {
		return nil, errors.New("GitLab returned invalid JSON")
	}
	return map[string]any{"data": json.RawMessage(body), "next_page": resp.Header.Get("X-Next-Page")}, nil
}

func (b bridge) reviewReminders(ctx context.Context, project string) (map[string]any, error) {
	var current struct {
		Username string `json:"username"`
	}
	if err := b.getJSON(ctx, "/api/v4/user", &current); err != nil {
		return nil, err
	}
	list, err := b.call(ctx, operation{name: "gitlab_merge_requests", suffix: "/merge_requests"}, query{Project: project})
	if err != nil {
		return nil, err
	}
	var items []struct {
		IID       int    `json:"iid"`
		Title     string `json:"title"`
		Draft     bool   `json:"draft"`
		WebURL    string `json:"web_url"`
		UpdatedAt string `json:"updated_at"`
		Reviewers []struct {
			Username string `json:"username"`
		} `json:"reviewers"`
	}
	if err := json.Unmarshal(list["data"].(json.RawMessage), &items); err != nil {
		return nil, errors.New("GitLab MR list is invalid")
	}
	reminders := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.Draft || strings.HasPrefix(strings.ToLower(item.Title), "draft:") {
			continue
		}
		reviewers := make([]string, 0, len(item.Reviewers))
		assigned := false
		for _, reviewer := range item.Reviewers {
			reviewers = append(reviewers, reviewer.Username)
			if reviewer.Username == current.Username {
				assigned = true
			}
		}
		if !assigned {
			continue
		}
		reminders = append(reminders, map[string]any{"id": fmt.Sprintf("gitlab:%s:%d:%s", project, item.IID, item.UpdatedAt), "project": project, "iid": item.IID, "title": item.Title, "url": item.WebURL, "updated_at": item.UpdatedAt, "reviewers": reviewers, "status": "open_non_draft", "approval_requirement": "unknown"})
	}
	return map[string]any{"reminders": reminders, "next_page": list["next_page"]}, nil
}

func (b bridge) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.gitlab+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", b.token)
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitLab returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(out)
}

func (b bridge) server() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "bobby-gitlab", Version: "0.1.0"}, nil)
	for _, op := range operations {
		mcp.AddTool(server, &mcp.Tool{Name: op.name, Description: op.description}, func(ctx context.Context, _ *mcp.CallToolRequest, in query) (*mcp.CallToolResult, map[string]any, error) {
			result, err := b.call(ctx, op, in)
			if err != nil {
				return nil, nil, err
			}
			return nil, result, nil
		})
	}
	return server
}
func (b bridge) handler() http.Handler {
	stream := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token, _ := b.authorize(r)
		scoped := b
		scoped.token = token
		return scoped.server()
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := b.authorize(r); !ok {
			http.Error(w, "GitLab binding unavailable", http.StatusForbidden)
			return
		}
		stream.ServeHTTP(w, r)
	})
}

func main() {
	if os.Getenv("GITLAB_MODE") == "mock" {
		log.Fatal(serveMock())
	}
	var tokens map[string]string
	if path := os.Getenv("GITLAB_DEMO_TOKENS_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(data, &tokens) != nil {
			log.Fatal("GitLab demo token map is invalid")
		}
	}
	secret := []byte{}
	if len(tokens) == 0 {
		var err error
		secret, err = os.ReadFile(os.Getenv("GITLAB_DEMO_TOKEN_FILE"))
		if err != nil {
			log.Fatal("GitLab demo token file is required")
		}
	}
	b := bridge{
		gitlab:    strings.TrimRight(os.Getenv("GITLAB_API_ORIGIN"), "/"),
		directory: strings.TrimRight(os.Getenv("TEAM_DIRECTORY_URL"), "/"),
		subject:   os.Getenv("GITLAB_DEMO_SUBJECT"), token: strings.TrimSpace(string(secret)), tokens: tokens,
		client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	for _, origin := range []string{b.gitlab, b.directory} {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			log.Fatal("provider origin must be an HTTP or HTTPS origin")
		}
	}
	if len(tokens) == 0 && (b.subject == "" || b.token == "") {
		log.Fatal("a demo subject and token are required")
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", b.handler())
	server := http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
