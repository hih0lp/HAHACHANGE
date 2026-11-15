package main

import (
	"context"
	"encoding/json"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScenarioRouteLifecycleWithVerifiedAccess(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	permission := "missing"
	requested := false
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/login" && r.Header.Get("Authorization") != "Bearer signed-token" {
			http.Error(w, "invalid token", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			json.NewEncoder(w).Encode(scenario.Profile{ID: "intern", CurrentTeamID: "team", Teams: []scenario.Team{{ID: "team"}}})
		case "/v1/teams/team/resources":
			json.NewEncoder(w).Encode(map[string]any{"resources": []scenario.Resource{{ID: "jira", Kind: "jira_project", Name: "Jira"}}})
		case "/v1/access/check":
			json.NewEncoder(w).Encode(scenario.Check{ResourceID: "jira", Capability: "browse_issues", State: permission})
		case "/v1/access-requests":
			requested = true
			if r.Header.Get("Idempotency-Key") != "stable-key" {
				t.Error("missing idempotency key")
			}
			json.NewEncoder(w).Encode(scenario.Request{ID: "request-1", Status: "pending"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()
	mux := http.NewServeMux()
	registerScenarioRoutes(mux, scenario.Store{DB: db}, scenario.Client{BaseURL: service.URL})
	call := func(method, path, token, body, idem string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Idempotency-Key", idem)
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("%s %s: got %d want %d body=%s", method, path, out.Code, want, out.Body.String())
		}
		return out
	}
	call("POST", "/v1/scenarios/login", "", "{", "", 400)
	call("POST", "/v1/scenarios", "wrong", "{}", "", 401)
	created := call("POST", "/v1/scenarios", "signed-token", "{}", "", 201)
	var state scenario.State
	if err := json.Unmarshal(created.Body.Bytes(), &state); err != nil || state.Widget.Type != "access_request_form" || state.Revision != 1 {
		t.Fatalf("created=%+v err=%v", state, err)
	}
	path := "/v1/scenarios/" + state.ID
	call("GET", path, "signed-token", "", "", 200)
	call("GET", "/v1/scenarios/00000000-0000-0000-0000-000000000000", "signed-token", "", "", 404)
	call("POST", path+"/access-requests", "signed-token", "{", "", 400)
	call("POST", path+"/access-requests", "signed-token", `{"revision":2,"reason":"Need Jira","confirmed":true}`, "stable-key", 409)
	call("POST", path+"/access-requests", "signed-token", `{"revision":1,"reason":"Need Jira","confirmed":true}`, "", 400)
	if requested {
		t.Fatal("unconfirmed request reached access service")
	}
	response := call("POST", path+"/access-requests", "signed-token", `{"revision":1,"reason":"Need Jira","confirmed":true}`, "stable-key", 200)
	if !requested {
		t.Fatal("confirmed request was not sent")
	}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || state.Status != "waiting_access" || state.Widget.RequestID != "request-1" {
		t.Fatalf("request state=%+v err=%v", state, err)
	}
	permission = "granted"
	response = call("POST", path+"/advance", "signed-token", `{"revision":2}`, "", 200)
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || state.Status != "ready_for_task" {
		t.Fatalf("advanced=%+v err=%v", state, err)
	}
	if _, err := (scenario.Store{DB: db}).Get(ctx, "other", state.ID); err == nil {
		t.Fatal("another owner could read the scenario")
	}
}

func TestScenarioRequiresVerifiedIdentity(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "invalid token", 401) }))
	defer service.Close()
	mux := http.NewServeMux()
	registerScenarioRoutes(mux, scenario.Store{}, scenario.Client{BaseURL: service.URL})
	for _, token := range []string{"", "Bearer forged"} {
		req := httptest.NewRequest("POST", "/v1/scenarios", strings.NewReader(`{}`))
		req.Header.Set("Authorization", token)
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		if out.Code != 401 {
			t.Fatalf("unverified identity reached scenario storage: %d", out.Code)
		}
	}
}
func TestScenarioLoginDoesNotCacheCredentials(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/login" {
			t.Error("wrong login path")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"signed-token","token_type":"Bearer","expires_in":900}`))
	}))
	defer service.Close()
	mux := http.NewServeMux()
	registerScenarioRoutes(mux, scenario.Store{}, scenario.Client{BaseURL: service.URL})
	req := httptest.NewRequest("POST", "/v1/scenarios/login", strings.NewReader(`{"username":"intern","password":"test"}`))
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, req)
	if out.Code != 200 || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe login response: %d", out.Code)
	}
}
