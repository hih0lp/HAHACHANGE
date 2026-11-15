package main

import (
	"net/http/httptest"
	"strings"
	"testing"
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
