package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func TestUpdateIssueFields(t *testing.T) {
	r := httptest.NewRequest("PUT", "/v1/mock-data/jira/issues/SCRUM-1", nil)
	r.SetPathValue("key", "SCRUM-1")
	data := []byte(`{"project_key":"SCRUM","issues":[{"key":"SCRUM-1","summary":"Old","status":"Open","description":"old","due_date":"2026-11-17"}]}`)
	changed, err := updateIssue(data, r, []byte(`{"summary":"New","description":"Details","due_date":"2026-11-20"}`))
	if err != nil || validateJiraFixture(changed) != nil || !strings.Contains(string(changed), `"summary":"New"`) || !strings.Contains(string(changed), `"due_date":"2026-11-20"`) {
		t.Fatalf("changed: %s %v", changed, err)
	}
	invalid, err := updateIssue(data, r, []byte(`{"due_date":"2026-13-20"}`))
	if err == nil && validateJiraFixture(invalid) == nil {
		t.Fatal("bad date accepted")
	}
}

func TestReadIssueReturnsSavedDescriptionOnlyToAssigneeOrMentor(t *testing.T) {
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" {
			t.Errorf("unexpected directory path %s", r.URL.Path)
		}
		account := map[string]string{"Bearer intern": "demo-intern", "Bearer intern2": "demo-intern2"}[r.Header.Get("Authorization")]
		if account == "" && r.Header.Get("Authorization") != "Bearer mentor" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		id := "intern"
		if account == "demo-intern2" {
			id = "intern2"
		} else if account == "" {
			id = "approver"
		}
		_ = json.NewEncoder(w).Encode(scenario.Profile{ID: id, JiraAccountID: account})
	}))
	defer directory.Close()
	data := fixture{ProjectKey: "SCRUM", Issues: []issue{{Key: "SCRUM-1", Summary: "Task", Status: "Open", Description: "Saved details", AssigneeAccountID: "demo-intern"}}}
	read := func(context.Context) (fixture, error) { return data, nil }
	for _, tc := range []struct {
		token string
		want  int
	}{
		{"intern", 200}, {"intern2", 404}, {"mentor", 200}, {"invalid", 403}, {"", 401},
	} {
		t.Run(tc.token, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/v1/mock-data/jira/issues/SCRUM-1", nil)
			r.SetPathValue("key", "SCRUM-1")
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			readIssue(w, r, read, scenario.Client{BaseURL: directory.URL})
			if w.Code != tc.want || tc.want == 200 && !strings.Contains(w.Body.String(), `"description":"Saved details"`) {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
		})
	}
}
