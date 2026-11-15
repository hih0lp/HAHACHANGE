package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hackchange/avito-rag/internal/mockfixture"
	"github.com/hackchange/avito-rag/internal/scenario"
)

const (
	mockPath   = "demo/bobby-playbook"
	mockSHA    = "7dc8e92987a7477e952bf08513d52f838a975db1"
	mockTime   = "2026-11-13T09:00:00Z"
	mockReadme = "# Bobby Playbook\n\nSynthetic GitLab data for the Bobby demo.\n"
)

type mockData struct {
	Projects []mockProject `json:"projects"`
}

type mockProject struct {
	ID            int          `json:"id"`
	Name          string       `json:"name"`
	Path          string       `json:"path_with_namespace"`
	DefaultBranch string       `json:"default_branch"`
	Description   string       `json:"description"`
	Owner         string       `json:"owner"`
	Branches      []mockBranch `json:"branches"`
	Commits       []mockCommit `json:"commits"`
	MRs           []mockMR     `json:"merge_requests"`
}

type mockBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	CommitID  string `json:"commit_id"`
}

type mockCommit struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Author  string `json:"author_name"`
	Created string `json:"created_at"`
}

type mockMR struct {
	IID         int    `json:"iid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
	Draft       bool   `json:"draft"`
	Source      string `json:"source_branch"`
	Target      string `json:"target_branch"`
	Updated     string `json:"updated_at"`
	Reviewer    string `json:"reviewer"`
	Approved    bool   `json:"approved"`
}

func defaultMockData() mockData {
	const title = "Document the demo workflow"
	return mockData{Projects: []mockProject{{
		ID: 2, Name: "Bobby Playbook (demo)", Path: mockPath, DefaultBranch: "main", Description: "Synthetic GitLab project",
		Branches: []mockBranch{{"main", true, mockSHA}, {"docs/demo", false, mockSHA}},
		Commits:  []mockCommit{{mockSHA, title, title, "Demo Maintainer", mockTime}},
		MRs:      []mockMR{{IID: 1, Title: title, Description: "Demo merge request for the onboarding playbook.", State: "opened", Source: "docs/demo", Target: "main", Updated: mockTime}},
	}}}
}

var projectPath = regexp.MustCompile("^[a-zA-Z0-9_-][a-zA-Z0-9_.-]*(/[a-zA-Z0-9_-][a-zA-Z0-9_.-]*)+$")
var commitID = regexp.MustCompile("^[a-f0-9]{40}$")
var branchName = regexp.MustCompile("^[a-zA-Z0-9_-][a-zA-Z0-9_./-]*$")

func parseMockData(data []byte) (mockData, error) {
	var f mockData
	if err := json.Unmarshal(data, &f); err != nil || f.Projects == nil {
		return f, fmt.Errorf("Требуется список проектов GitLab.")
	}
	ids := map[int]bool{}
	paths := map[string]bool{}
	for _, p := range f.Projects {
		path := strings.ToLower(p.Path)
		if p.ID <= 0 || ids[p.ID] || paths[path] || strings.TrimSpace(p.Name) == "" || !projectPath.MatchString(p.Path) ||
			(p.Owner != "" && p.Owner != "intern" && p.Owner != "intern2") {
			return f, fmt.Errorf("Некорректный или повторяющийся проект %q.", p.Path)
		}
		ids[p.ID], paths[path] = true, true
		commits := map[string]bool{}
		for _, v := range p.Commits {
			if _, err := time.Parse(time.RFC3339, v.Created); err != nil || !commitID.MatchString(v.ID) || commits[v.ID] || strings.TrimSpace(v.Title) == "" {
				return f, fmt.Errorf("Проверьте SHA, название и дату коммита в %s.", p.Path)
			}
			commits[v.ID] = true
		}
		branches := map[string]bool{}
		for _, v := range p.Branches {
			if !branchName.MatchString(v.Name) || branches[v.Name] || (v.CommitID != "" && !commits[v.CommitID]) {
				return f, fmt.Errorf("Проверьте имя ветки %q и выбранный коммит.", v.Name)
			}
			branches[v.Name] = true
		}
		if !branches[p.DefaultBranch] {
			return f, fmt.Errorf("Основная ветка %q должна существовать в %s.", p.DefaultBranch, p.Path)
		}
		mrs := map[int]bool{}
		for _, v := range p.MRs {
			_, timeErr := time.Parse(time.RFC3339, v.Updated)
			if v.IID <= 0 || mrs[v.IID] || strings.TrimSpace(v.Title) == "" || timeErr != nil || !branches[v.Source] || !branches[v.Target] || v.Source == v.Target ||
				(v.State != "opened" && v.State != "closed" && v.State != "merged") {
				return f, fmt.Errorf("Проверьте название, ветки, статус и дату MR !%d.", v.IID)
			}
			mrs[v.IID] = true
		}
	}
	return f, nil
}

func serveMock() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := mockfixture.OpenDatabase(ctx, os.Getenv("MOCK_DATABASE_URL"))
	if err != nil {
		return err
	}
	if db != nil {
		defer db.Close()
	}
	dataset, err := mockfixture.Seed(ctx, db, "gitlab", func() ([]byte, error) { return json.Marshal(defaultMockData()) }, func(data []byte) error {
		_, err := parseMockData(data)
		return err
	})
	if err != nil {
		return err
	}
	cancel()
	read := func(ctx context.Context) (mockData, error) {
		if dataset == nil {
			return defaultMockData(), nil
		}
		data, err := dataset.Bytes(ctx)
		if err != nil {
			return mockData{}, err
		}
		return parseMockData(data)
	}
	directory := os.Getenv("TEAM_DIRECTORY_URL")
	if directory == "" {
		directory = "http://team-access:8080"
	}
	b := bridge{gitlab: "http://127.0.0.1:8080", directory: directory, mock: true, client: &http.Client{
		Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	mux := http.NewServeMux()
	mux.Handle("/mcp", b.handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := read(r.Context()); err != nil {
			http.Error(w, "GitLab data unavailable", 503)
			return
		}
		w.WriteHeader(200)
	})
	if dataset != nil {
		dataset.Register(mux, scenario.Client{BaseURL: directory})
		dataset.RegisterMutation(mux, scenario.Client{BaseURL: directory}, "/v1/mock-data/gitlab/projects/{project}/branches/{name...}", renameMockBranch)
	}
	mux.HandleFunc("GET /api/v4/", func(w http.ResponseWriter, r *http.Request) {
		serveMockGitLab(w, r, read)
	})
	return (&http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe()
}

func mockGitLab(w http.ResponseWriter, r *http.Request) {
	serveMockGitLab(w, r, func(context.Context) (mockData, error) { return defaultMockData(), nil })
}

func serveMockGitLab(w http.ResponseWriter, r *http.Request, read func(context.Context) (mockData, error)) {
	subject := r.Header.Get("PRIVATE-TOKEN")
	if !strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") || subject == "" {
		http.Error(w, "Forbidden", 403)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4/"), "/")
	if len(parts) == 1 && parts[0] == "user" {
		mockJSON(w, map[string]any{"username": subject, "name": subject})
		return
	}
	if parts[0] != "projects" {
		mockNotFound(w)
		return
	}
	f, err := read(r.Context())
	if err != nil {
		http.Error(w, "GitLab data unavailable", 503)
		return
	}
	if len(parts) == 1 {
		projects := []map[string]any{}
		for _, p := range f.Projects {
			if p.visible(subject) {
				projects = append(projects, map[string]any{"id": p.ID, "name": p.Name, "path_with_namespace": p.Path, "default_branch": p.DefaultBranch, "description": p.Description})
			}
		}
		mockJSON(w, projects)
		return
	}
	if len(parts) < 3 {
		mockNotFound(w)
		return
	}
	projectID, _ := url.PathUnescape(parts[1])
	for _, p := range f.Projects {
		if (projectID == strconv.Itoa(p.ID) || strings.EqualFold(projectID, p.Path)) && p.visible(subject) {
			p.serve(w, r, strings.Join(parts[2:], "/"), subject)
			return
		}
	}
	mockNotFound(w)
}

func (p mockProject) visible(subject string) bool {
	return p.Owner == "" || subject == p.Owner || subject == "approver"
}

func (p mockProject) branch(name string) (mockBranch, bool) {
	for _, b := range p.Branches {
		if name == b.Name {
			return b, true
		}
	}
	return mockBranch{}, false
}

func (p mockProject) commit(id string) (mockCommit, bool) {
	for _, c := range p.Commits {
		if c.ID == id || (len(id) >= 8 && strings.HasPrefix(c.ID, id)) {
			return c, true
		}
	}
	return mockCommit{}, false
}

func (p mockProject) knownRef(ref string) bool {
	if ref == "" || ref == "HEAD" || p.ID == 2 && ref == "v1.0.0" {
		return true
	}
	_, branch := p.branch(ref)
	_, commit := p.commit(ref)
	return branch || commit
}

func (p mockProject) serve(w http.ResponseWriter, r *http.Request, path, subject string) {
	if path == "repository/files/README.md/raw" && p.ID == 2 && p.knownRef(r.URL.Query().Get("ref")) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(mockReadme))
		return
	}
	result := p.result(path, r, subject)
	if result == nil {
		mockNotFound(w)
		return
	}
	mockJSON(w, result)
}

func (p mockProject) result(path string, r *http.Request, subject string) any {
	q := r.URL.Query()
	empty := []any{}
	switch path {
	case "repository/tree":
		if !p.knownRef(q.Get("ref")) || q.Get("path") != "" {
			return nil
		}
		if p.ID == 2 {
			return []any{map[string]any{"id": mockSHA, "name": "README.md", "type": "blob", "path": "README.md", "mode": "100644"}}
		}
		return empty
	case "repository/branches":
		branches := []any{}
		for _, b := range p.Branches {
			entry := map[string]any{"name": b.Name, "default": b.Name == p.DefaultBranch, "protected": b.Protected}
			if c, ok := p.commit(b.CommitID); ok {
				entry["commit"] = c.response()
			}
			branches = append(branches, entry)
		}
		return branches
	case "repository/tags":
		if c, ok := p.commit(mockSHA); p.ID == 2 && ok {
			return []any{map[string]any{"name": "v1.0.0", "commit": c.response()}}
		}
		return empty
	case "repository/commits":
		ref := q.Get("ref_name")
		if !p.knownRef(ref) || (q.Get("path") != "" && (p.ID != 2 || q.Get("path") != "README.md")) {
			return empty
		}
		if ref == "HEAD" {
			ref = p.DefaultBranch
		}
		if branch, ok := p.branch(ref); ok {
			ref = branch.CommitID
			if ref == "" {
				return empty
			}
		}
		if ref == "v1.0.0" {
			ref = mockSHA
		}
		if c, matched := p.commit(ref); matched {
			return []any{c.response()}
		}
		commits := []any{}
		for _, c := range p.Commits {
			commits = append(commits, c.response())
		}
		return commits
	case "merge_requests":
		mrs := []any{}
		state := q.Get("state")
		for _, mr := range p.MRs {
			if state == "" || state == "all" || state == mr.State {
				mrs = append(mrs, p.mrResponse(mr, subject))
			}
		}
		return mrs
	case "pipelines":
		ref := q.Get("ref")
		if ref == "" {
			ref = p.DefaultBranch
		}
		if _, ok := p.branch(ref); p.ID == 2 && ok {
			return []any{map[string]any{"id": 201, "iid": 1, "project_id": 2, "sha": mockSHA, "ref": ref, "status": "success", "updated_at": mockTime}}
		}
		return empty
	}
	if id, ok := strings.CutPrefix(path, "repository/commits/"); ok && strings.HasSuffix(id, "/diff") {
		if c, ok := p.commit(strings.TrimSuffix(id, "/diff")); ok {
			if p.ID == 2 && c.ID == mockSHA {
				return mockDiff()
			}
			return empty
		}
	}
	if rest, ok := strings.CutPrefix(path, "merge_requests/"); ok {
		id, action, _ := strings.Cut(rest, "/")
		for _, mr := range p.MRs {
			if id == strconv.Itoa(mr.IID) {
				return p.mrDetails(mr, action, subject)
			}
		}
	}
	return nil
}

func (c mockCommit) response() map[string]any {
	return map[string]any{"id": c.ID, "short_id": c.ID[:8], "title": c.Title, "message": c.Message, "author_name": c.Author, "created_at": c.Created}
}

func mockReviewer(mr mockMR, subject string) map[string]string {
	reviewer := mr.Reviewer
	if reviewer == "" {
		reviewer = subject
	}
	return map[string]string{"username": reviewer, "name": reviewer}
}

func (p mockProject) mrResponse(mr mockMR, subject string) map[string]any {
	id := p.ID*1000000 + mr.IID
	if p.ID == 2 && mr.IID == 1 {
		id = 101
	}
	return map[string]any{"id": id, "iid": mr.IID, "project_id": p.ID, "title": mr.Title,
		"description": mr.Description, "state": mr.State, "draft": mr.Draft, "source_branch": mr.Source, "target_branch": mr.Target,
		"updated_at": mr.Updated, "reviewers": []any{mockReviewer(mr, subject)}, "references": map[string]any{"full": p.Path + "!" + strconv.Itoa(mr.IID)}}
}
func (p mockProject) mrDetails(mr mockMR, action, subject string) any {
	reviewer := mockReviewer(mr, subject)
	votes := []any{}
	left := 1
	if mr.Approved {
		votes = append(votes, map[string]any{"user": reviewer})
		left = 0
	}
	switch action {
	case "":
		return p.mrResponse(mr, subject)
	case "notes":
		if p.ID == 2 && mr.IID == 1 {
			return []any{map[string]any{"id": 1, "body": "Please review the demo README.", "author": map[string]any{"username": "demo-maintainer"}, "created_at": mockTime, "system": false}}
		}
		return []any{}
	case "diffs":
		if p.ID == 2 && mr.IID == 1 {
			return mockDiff()
		}
		return []any{}
	case "approval_state":
		return map[string]any{"rules": []any{map[string]any{"id": 1, "name": "Demo review", "approvals_required": 1, "approved": mr.Approved, "approved_by": votes, "eligible_approvers": []any{reviewer}}}}
	case "approvals":
		return map[string]any{"approved": mr.Approved, "approvals_required": 1, "approvals_left": left, "approved_by": votes}
	default:
		return nil
	}
}

func mockDiff() []any {
	return []any{map[string]any{"old_path": "README.md", "new_path": "README.md", "new_file": false, "renamed_file": false, "deleted_file": false, "diff": "@@ -1 +1,3 @@\n # Bobby Playbook\n+\n+Synthetic GitLab data for the Bobby demo.\n"}}
}

func mockJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func mockNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "404 Not Found"})
}
