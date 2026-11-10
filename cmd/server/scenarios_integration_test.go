package main

import (
	"encoding/json"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScenarioHTTPWorkflow(t *testing.T) {
	db := testsupport.Postgres(t)
	permission := "missing"
	requests := 0
	serviceDown := false
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serviceDown {
			http.Error(w, "down", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			id := "alice"
			if r.Header.Get("Authorization") == "Bearer bob" {
				id = "bob"
			}
			json.NewEncoder(w).Encode(scenario.Profile{ID: id, CurrentTeamID: "team", Teams: []scenario.Team{{ID: "team"}}})
		case "/v1/teams/team/resources":
			json.NewEncoder(w).Encode(map[string]any{"resources": []scenario.Resource{{ID: "jira", Kind: "jira_project", Name: "Team Jira"}}})
		case "/v1/access/check":
			json.NewEncoder(w).Encode(scenario.Check{ResourceID: "jira", Capability: "browse_issues", State: permission, RequestID: "req-1"})
		case "/v1/access-requests":
			requests++
			if r.Header.Get("Idempotency-Key") == "remote-error" {
				http.Error(w, "conflict", 409)
				return
			}
			permission = "pending"
			json.NewEncoder(w).Encode(scenario.Request{ID: "req-1", Status: "pending"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()
	mux := http.NewServeMux()
	registerScenarioRoutes(mux, scenario.Store{DB: db}, scenario.Client{BaseURL: service.URL})
	headers := map[string]string{"Authorization": "Bearer alice", "Idempotency-Key": "stable-key"}
	out := ownedRequest(mux, "POST", "/v1/scenarios", "{}", "", headers)
	if out.Code != 201 {
		t.Fatal(out.Body.String())
	}
	var s scenario.State
	json.Unmarshal(out.Body.Bytes(), &s)
	path := "/v1/scenarios/" + s.ID
	if s.Widget.Type != "access_request_form" {
		t.Fatal("no form")
	}
	if out := ownedRequest(mux, "GET", path, "", "", map[string]string{"Authorization": "Bearer bob"}); out.Code != 404 {
		t.Fatal("foreign scenario exposed")
	}
	if out := ownedRequest(mux, "GET", path, "", "", headers); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	for _, tc := range []struct {
		suffix, body string
		code         int
	}{{"advance", "{", 400}, {"advance", `{"revision":0}`, 409}, {"access-requests", `{"revision":1}`, 400}} {
		if out := ownedRequest(mux, "POST", path+"/"+tc.suffix, tc.body, "", headers); out.Code != tc.code {
			t.Fatalf("%+v got %d %s", tc, out.Code, out.Body.String())
		}
	}
	headers["Idempotency-Key"] = "remote-error"
	if out := ownedRequest(mux, "POST", path+"/access-requests", `{"revision":1,"reason":"first task","confirmed":true}`, "", headers); out.Code != 409 {
		t.Fatal("remote conflict hidden")
	}
	headers["Idempotency-Key"] = "stable-key"
	out = ownedRequest(mux, "POST", path+"/access-requests", `{"revision":1,"reason":"first task","confirmed":true}`, "", headers)
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	json.Unmarshal(out.Body.Bytes(), &s)
	if s.Revision != 2 || s.Status != "waiting_access" || s.Widget.RequestID != "req-1" || requests != 2 {
		t.Fatal("request result not saved")
	}
	if out := ownedRequest(mux, "POST", path+"/access-requests", `{"revision":2,"reason":"first task","confirmed":true}`, "", headers); out.Code != 409 {
		t.Fatal("action repeated outside current step")
	}
	permission = "granted"
	out = ownedRequest(mux, "POST", path+"/advance", `{"revision":2}`, "", headers)
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	json.Unmarshal(out.Body.Bytes(), &s)
	if s.Revision != 3 || s.Status != "ready_for_task" {
		t.Fatal("did not advance after real check")
	}
	serviceDown = true
	if out := ownedRequest(mux, "POST", path+"/advance", `{"revision":3}`, "", headers); out.Code != 503 {
		t.Fatal("dependency outage hidden")
	}
	if out := ownedRequest(mux, "POST", "/v1/scenarios/login", "{", "", nil); out.Code != 400 {
		t.Fatal("invalid login accepted")
	}
	serviceDown = false
	db.Close()
	if out := ownedRequest(mux, "POST", "/v1/scenarios", "{}", "", headers); out.Code != 503 {
		t.Fatal("database outage hidden")
	}
}

func TestOnboardingPage(t *testing.T) {
	out := httptest.NewRecorder()
	onboardingPage(out, httptest.NewRequest("GET", "/onboarding", nil))
	if out.Header().Get("Cache-Control") != "no-store" || !strings.Contains(out.Body.String(), "access_request_form") {
		t.Fatal("missing secure onboarding response")
	}
}
