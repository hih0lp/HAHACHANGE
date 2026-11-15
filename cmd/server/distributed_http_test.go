package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestJobHTTPChecksIdentityAndKeepsEditsScopedToOwner(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	if err := distributed.EnsureChatSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE ai_jobs ADD COLUMN IF NOT EXISTS team_context jsonb"); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "expired", http.StatusUnauthorized)
	}))
	defer backend.Close()
	t.Setenv("TEAM_ACCESS_URL", backend.URL)
	jobs := &distributed.System{DB: db}
	mux := http.NewServeMux()
	registerDistributedJobRoutes(mux, jobs, "")
	request := func(method, path, owner, body, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, owner)))
		if w.Code != want {
			t.Fatalf("%s %s: status %d want %d, %s", method, path, w.Code, want, w.Body)
		}
		return w
	}
	for _, body := range []string{`{`, `{"query":"x"}`, `{"query":"question","scenario":"unknown"}`, `{"query":"question","thread_id":"other"}`} {
		request("POST", "/v1/jobs", "alice", body, "", 400)
	}
	request("POST", "/v1/jobs", "alice", `{"query":"question"}`, "expired", 503)
	created := request("POST", "/v1/jobs", "alice", `{"query":"  question  "}`, "", 202)
	var job distributed.Job
	if err := json.Unmarshal(created.Body.Bytes(), &job); err != nil || job.Query != "question" || job.Scenario != "general" {
		t.Fatalf("%s %v", created.Body, err)
	}
	path := "/v1/jobs/" + job.ID
	if created.Header().Get("Location") != path {
		t.Fatal(created.Header())
	}
	request("GET", path, "bob", "", "", 404)
	request("POST", path+"/stop", "bob", `{}`, "", 404)
	request("POST", path+"/edit", "bob", `{"query":"different"}`, "", 404)
	request("POST", path+"/edit", "alice", `{`, "", 400)
	request("POST", path+"/edit", "alice", `{"query":"x"}`, "", 400)
	request("POST", path+"/edit", "alice", `{"query":"different"}`, "expired", 503)
	stopped := request("POST", path+"/stop", "alice", `{}`, "", 200)
	if !strings.Contains(stopped.Body.String(), "Остановлено пользователем") {
		t.Fatal(stopped.Body)
	}
	edited := request("POST", path+"/edit", "alice", `{"query":"  новое уточнение  "}`, "", 202)
	if edited.Header().Get("Location") == path || !strings.Contains(edited.Body.String(), "новое уточнение") {
		t.Fatal(edited.Body)
	}
	request("GET", edited.Header().Get("Location"), "alice", "", "", 200)
	if _, err := db.Exec(ctx, "DROP TABLE ai_jobs CASCADE"); err != nil {
		t.Fatal(err)
	}
	request("GET", path, "alice", "", "", 503)
	request("POST", path+"/stop", "alice", `{}`, "", 503)
	request("POST", path+"/edit", "alice", `{"query":"different"}`, "", 503)
	request("POST", "/v1/jobs", "alice", `{"query":"question"}`, "", 503)
}
