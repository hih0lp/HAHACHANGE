package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func ownedRequest(mux http.Handler, method, path, body, owner string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ownerKey{}, owner))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, req)
	return out
}
func TestDemoCookieIntegrity(t *testing.T) {
	h := demoSession("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(ownerFrom(r))) }))
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest("GET", "/", nil))
	cookies := first.Result().Cookies()
	if len(cookies) != 1 || len(first.Body.String()) != 36 {
		t.Fatal("no session")
	}
	req := httptest.NewRequest("GET", "/v1/jobs", nil)
	req.AddCookie(cookies[0])
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Body.String() != first.Body.String() {
		t.Fatal("owner changed")
	}
	for _, value := range []string{"", strings.Repeat("a", 36) + ".zz", strings.Repeat("a", 36) + "." + strings.Repeat("0", 64)} {
		req := httptest.NewRequest("GET", "/v1/jobs", nil)
		req.AddCookie(&http.Cookie{Name: "ai_demo_session", Value: value})
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != 401 {
			t.Fatal("forged cookie accepted")
		}
	}
	for _, path := range []string{"/onboarding", "/v1/scenarios", "/v1/scenarios/id"} {
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest("GET", path, nil))
		if out.Code != 200 {
			t.Fatal("JWT route blocked by anonymous cookie")
		}
	}
}

func TestDistributedHTTPAndWebSocket(t *testing.T) {
	db := testsupport.Postgres(t)
	system := &distributed.System{DB: db, MaxTries: 1}
	mux := http.NewServeMux()
	registerDistributedJobRoutes(mux, system, "key")
	headers := map[string]string{"X-API-Key": "key"}
	for _, body := range []string{"{", `{"query":"x"}`, `{"query":"hello","scenario":"unknown"}`} {
		if out := ownedRequest(mux, "POST", "/v1/jobs", body, "alice", headers); out.Code != 400 {
			t.Fatalf("invalid input accepted %d", out.Code)
		}
	}
	out := ownedRequest(mux, "POST", "/v1/jobs", `{"query":"hello"}`, "alice", headers)
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	var j distributed.Job
	json.Unmarshal(out.Body.Bytes(), &j)
	if out := ownedRequest(mux, "GET", "/v1/jobs/"+j.ID, "", "bob", headers); out.Code != 404 {
		t.Fatal("foreign job exposed")
	}
	if out := ownedRequest(mux, "GET", "/v1/jobs/"+j.ID, "", "alice", headers); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, "alice")))
	}))
	defer server.Close()
	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/jobs/"+j.ID+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		return c
	}
	bad := dial()
	bad.WriteJSON(map[string]string{"api_key": "wrong"})
	if bad.ReadJSON(&j) == nil {
		t.Fatal("bad websocket auth accepted")
	}
	bad.Close()
	conn := dial()
	defer conn.Close()
	conn.WriteJSON(map[string]string{"api_key": "key"})
	var snapshot distributed.Job
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "queued" {
		t.Fatal(snapshot.Status)
	}
	if err := system.ProcessOne(context.Background(), j.ID, func(context.Context, string) (string, any, error) { return "done", nil, nil }, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&snapshot); err != nil || snapshot.Status != "completed" {
		t.Fatalf("missing completion %+v %v", snapshot, err)
	}
	terminal := dial()
	terminal.WriteJSON(map[string]string{"api_key": "key"})
	if err := terminal.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	terminal.Close()
	// Client disconnect during a running job must release the handler.
	another, _ := system.Submit(context.Background(), "alice", "general", "another")
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/jobs/"+another.ID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.WriteJSON(map[string]string{"api_key": "key"})
	c.ReadJSON(&snapshot)
	c.Close()
	if out := ownedRequest(mux, "GET", "/v1/jobs/"+j.ID+"/ws", "", "alice", headers); out.Code != 400 {
		t.Fatal("non websocket upgrade accepted")
	}
	db.Close()
	if out := ownedRequest(mux, "POST", "/v1/jobs", `{"query":"hello"}`, "alice", headers); out.Code != 503 {
		t.Fatal("db failure swallowed")
	}
	if out := ownedRequest(mux, "GET", "/v1/jobs/"+j.ID, "", "alice", headers); out.Code != 503 {
		t.Fatal("db read failure swallowed")
	}
}

func TestDistributedAccessDurabilityAndUncertainOutcome(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	system := &distributed.System{DB: db, MaxTries: 1}
	j, _ := system.Submit(ctx, "alice", "access", "need access")
	system.ProcessOne(ctx, j.ID, func(context.Context, string) (string, any, error) { return "answer", nil, nil }, time.Second)
	calls := 0
	fail := false
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"fields":{"status":{"name":"Open"}}}`)
			return
		}
		calls++
		if fail {
			http.Error(w, "unavailable", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"key":"TEST-1"}`)
	}))
	defer jira.Close()
	mux := http.NewServeMux()
	registerDistributedAccessRoutes(mux, "", jiraConfig{Mode: "live", BaseURL: jira.URL, ProjectKey: "TEST"}, system)
	headers := map[string]string{"Idempotency-Key": "unique-key-1", "X-Request-ID": j.ID}
	body := `{"resource":"repo","reason":"first task","confirmed":true}`
	for _, input := range []string{"{", `{"resource":"x"}`} {
		if out := ownedRequest(mux, "POST", "/v1/access-requests", input, "alice", headers); out.Code != 400 {
			t.Fatal(out.Code)
		}
	}
	if out := ownedRequest(mux, "POST", "/v1/access-requests", body, "alice", nil); out.Code != 400 {
		t.Fatal("missing idem accepted")
	}
	if out := ownedRequest(mux, "POST", "/v1/access-requests", body, "bob", headers); out.Code != 400 {
		t.Fatal("foreign source job accepted")
	}
	out := ownedRequest(mux, "POST", "/v1/access-requests", body, "alice", headers)
	if out.Code != 201 {
		t.Fatal(out.Body.String())
	}
	var action accessRequest
	json.Unmarshal(out.Body.Bytes(), &action)
	if out := ownedRequest(mux, "POST", "/v1/access-requests", body, "alice", headers); out.Code != 200 || calls != 1 {
		t.Fatal("duplicate external action")
	}
	if out := ownedRequest(mux, "POST", "/v1/access-requests", strings.Replace(body, "repo", "other", 1), "alice", headers); out.Code != 409 {
		t.Fatal("idempotency payload conflict ignored")
	}
	if out := ownedRequest(mux, "GET", "/v1/access-requests/"+action.ID, "", "alice", nil); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	if out := ownedRequest(mux, "GET", "/v1/access-requests/"+action.ID, "", "bob", nil); out.Code != 404 {
		t.Fatal("foreign action exposed")
	}
	if out := ownedRequest(mux, "GET", "/v1/access-requests", "", "alice", nil); out.Code != 200 || !strings.Contains(out.Body.String(), action.ID) {
		t.Fatal("saved action not listed")
	}
	fail = true
	headers["Idempotency-Key"] = "unique-key-2"
	if out := ownedRequest(mux, "POST", "/v1/access-requests", body, "alice", headers); out.Code != 502 {
		t.Fatal(out.Body.String())
	}
	if out := ownedRequest(mux, "POST", "/v1/access-requests", body, "alice", headers); out.Code != 409 || calls != 2 {
		t.Fatal("uncertain action was retried unsafely")
	}
	db.Close()
	if out := ownedRequest(mux, "GET", "/v1/access-requests", "", "alice", nil); out.Code != 503 {
		t.Fatal("db error hidden")
	}
}
