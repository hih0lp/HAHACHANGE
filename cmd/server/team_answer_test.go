package main

import (
	"strings"
	"testing"
)

func TestFormatTeamMembersUsesDirectoryRelationships(t *testing.T) {
	result := map[string]any{"team_id": "private-id", "members": []any{
		map[string]any{"id": "internal-1", "display_name": "Первый участник", "role": "approver", "jira_account_id": "secret-id", "is_mentor": true, "is_lead": true},
		map[string]any{"id": "internal-2", "display_name": "Второй участник", "role": "intern", "is_mentor": false, "is_lead": false},
	}}
	answer, err := formatTeamMembers(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Первый участник", "Второй участник", "Наставник: Первый участник", "Руководитель: Первый участник"} {
		if !strings.Contains(answer, want) {
			t.Fatalf("missing %q in %q", want, answer)
		}
	}
	for _, unexpected := range []string{"private-id", "internal-1", "secret-id", "approver", "intern"} {
		if strings.Contains(answer, unexpected) {
			t.Fatalf("internal detail %q leaked in %q", unexpected, answer)
		}
	}
}

func TestFormatTeamMembersRejectsIncompleteData(t *testing.T) {
	for _, result := range []any{map[string]any{}, map[string]any{"members": []any{map[string]any{"display_name": "Участник"}}}} {
		if _, err := formatTeamMembers(result); err == nil {
			t.Fatalf("accepted incomplete response: %#v", result)
		}
	}
}
