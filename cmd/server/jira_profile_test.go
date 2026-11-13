package main

import "testing"

func TestVerifiedJiraAccountID(t *testing.T) {
	profile := `{"source":"team-access","profile":{"id":"intern","jira_account_id":"account-123"}}`
	if got := verifiedJiraAccountID(profile); got != "account-123" {
		t.Fatalf("account=%q", got)
	}
	if got := verifiedJiraAccountID(`{"profile":{}}`); got != "" {
		t.Fatalf("missing account=%q", got)
	}
	if got := verifiedJiraAccountID(`{"id":"intern","jira_account_id":"account-456"}`); got != "account-456" {
		t.Fatalf("MCP account=%q", got)
	}
}
