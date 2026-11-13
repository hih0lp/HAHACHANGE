package main

import "testing"

func TestTeamEvidenceOmitsUnusableProfileLinks(t *testing.T) {
	data := map[string]any{"members": []any{map[string]any{"display_name": "Стажёр", "jira_account_id": "account", "jira_profile_url": "http://container/jira/people/account"}}}
	clean := withoutTeamProfileLinks(data).(map[string]any)
	member := clean["members"].([]any)[0].(map[string]any)
	if member["display_name"] != "Стажёр" || member["jira_account_id"] != "account" {
		t.Fatalf("membership lost: %#v", member)
	}
	if _, exists := member["jira_profile_url"]; exists {
		t.Fatalf("unusable profile link leaked: %#v", member)
	}
	if _, exists := data["members"].([]any)[0].(map[string]any)["jira_profile_url"]; !exists {
		t.Fatal("source data was mutated")
	}
}
