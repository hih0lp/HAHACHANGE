package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
)

func serveMock() error {
	b := bridge{
		gitlab: "http://127.0.0.1:8080", directory: "http://team-access:8080",
		mock: true, client: &http.Client{
			Timeout:       8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", b.handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /api/v4/", mockGitLab)
	return (&http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe()
}

const (
	mockPath   = "demo/bobby-playbook"
	mockSHA    = "7dc8e92987a7477e952bf08513d52f838a975db1"
	mockTime   = "2026-11-13T09:00:00Z"
	mockReadme = "# Bobby Playbook\n\nSynthetic GitLab data for the Bobby demo.\n"
)

func mockGitLab(w http.ResponseWriter, r *http.Request) {
	// The bridge uses loopback for REST and sends the authenticated demo identity.
	if !strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") || r.Header.Get("PRIVATE-TOKEN") == "" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	subject := r.Header.Get("PRIVATE-TOKEN")
	path := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4/"), "/")
	if len(path) == 1 && path[0] == "user" {
		mockJSON(w, map[string]any{"username": subject, "name": subject})
		return
	}
	project := map[string]any{"id": 2, "name": "Bobby Playbook (demo)", "path_with_namespace": mockPath, "default_branch": "main", "description": "Synthetic GitLab project"}
	if len(path) == 1 && path[0] == "projects" {
		mockJSON(w, []any{project})
		return
	}
	if len(path) < 3 || path[0] != "projects" || (path[1] != "2" && !strings.EqualFold(path[1], "demo%2Fbobby-playbook")) {
		mockNotFound(w)
		return
	}
	if strings.Join(path[2:], "/") == "repository/files/README.md/raw" {
		if !mockRef(r.URL.Query().Get("ref")) {
			mockNotFound(w)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(mockReadme))
		return
	}
	if result := mockProjectResult(strings.Join(path[2:], "/"), r, subject); result != nil {
		mockJSON(w, result)
		return
	}
	mockNotFound(w)
}

func mockRef(ref string) bool {
	return slices.Contains([]string{"", "main", "docs/demo", "v1.0.0", mockSHA, "HEAD"}, ref)
}

func mockProjectResult(path string, r *http.Request, subject string) any {
	q := r.URL.Query()
	empty := []any{}
	commit := map[string]any{"id": mockSHA, "short_id": mockSHA[:8], "title": "Document the demo workflow", "message": "Document the demo workflow", "author_name": "Demo Maintainer", "created_at": mockTime}
	diff := []any{map[string]any{"old_path": "README.md", "new_path": "README.md", "new_file": false, "renamed_file": false, "deleted_file": false, "diff": "@@ -1 +1,3 @@\n # Bobby Playbook\n+\n+Synthetic GitLab data for the Bobby demo.\n"}}
	reviewer := map[string]any{"username": subject, "name": subject}
	mr := map[string]any{"id": 101, "iid": 1, "project_id": 2, "title": "Document the demo workflow", "description": "Demo merge request for the onboarding playbook.", "state": "opened", "draft": false, "source_branch": "docs/demo", "target_branch": "main", "updated_at": mockTime, "reviewers": []any{reviewer}, "references": map[string]any{"full": mockPath + "!1"}}
	switch path {
	case "repository/tree":
		if !mockRef(q.Get("ref")) || q.Get("path") != "" {
			return nil
		}
		return []any{map[string]any{"id": mockSHA, "name": "README.md", "type": "blob", "path": "README.md", "mode": "100644"}}
	case "repository/branches":
		return []any{map[string]any{"name": "main", "default": true, "protected": true, "commit": commit}, map[string]any{"name": "docs/demo", "default": false, "commit": commit}}
	case "repository/tags":
		return []any{map[string]any{"name": "v1.0.0", "commit": commit}}
	case "repository/commits":
		if !mockRef(q.Get("ref_name")) || (q.Get("path") != "" && q.Get("path") != "README.md") {
			return empty
		}
		return []any{commit}
	case "repository/commits/" + mockSHA + "/diff", "repository/commits/" + mockSHA[:8] + "/diff", "merge_requests/1/diffs":
		return diff
	case "merge_requests":
		if state := q.Get("state"); state != "" && state != "opened" && state != "all" {
			return empty
		}
		return []any{mr}
	case "merge_requests/1":
		return mr
	case "merge_requests/1/notes":
		return []any{map[string]any{"id": 1, "body": "Please review the demo README.", "author": map[string]any{"username": "demo-maintainer"}, "created_at": mockTime, "system": false}}
	case "merge_requests/1/approval_state":
		return map[string]any{"rules": []any{map[string]any{"id": 1, "name": "Demo review", "approvals_required": 1, "approved": false, "approved_by": empty, "eligible_approvers": []any{reviewer}}}}
	case "merge_requests/1/approvals":
		return map[string]any{"approved": false, "approvals_required": 1, "approvals_left": 1, "approved_by": empty}
	case "pipelines":
		if ref := q.Get("ref"); ref != "" && ref != "main" && ref != "docs/demo" {
			return empty
		}
		ref := q.Get("ref")
		if ref == "" {
			ref = "main"
		}
		return []any{map[string]any{"id": 201, "iid": 1, "project_id": 2, "sha": mockSHA, "ref": ref, "status": "success", "updated_at": mockTime}}
	default:
		return nil
	}
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
