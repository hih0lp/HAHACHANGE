package main

import (
	"strings"
	"testing"
)

func TestFormatMockJiraCardPreservesMissingFields(t *testing.T) {
	card, err := formatMockJiraCard(map[string]any{"key": "DEMO-1", "summary": "Task", "status": "Open", "description": "", "url": "https://jira.example/browse/DEMO-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(card, "https://") {
		t.Fatalf("card exposed Jira URL: %q", card)
	}
	for _, want := range []string{"DEMO-1", "Дата начала: не указана", "Срок: не указан", "Описание: не заполнено"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card %q lacks %q", card, want)
		}
	}
}

func TestFormatMockJiraListUsesOnlyReturnedFields(t *testing.T) {
	result := map[string]any{"project_key": "DEMO", "issues": []any{map[string]any{"key": "DEMO-1", "summary": "Task", "status": "Open", "url": "https://jira.example/browse/DEMO-1"}}, "truncated": false}
	answer, err := formatMockJiraList(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"Описание", "первая", "инструмент", "дата начала", "https://"} {
		if strings.Contains(answer, unwanted) {
			t.Fatalf("list invented detail: %q", answer)
		}
	}
	if !strings.Contains(answer, "DEMO-1") || !strings.Contains(answer, "статус: Open") {
		t.Fatalf("missing returned issue: %q", answer)
	}
	if _, err := formatMockJiraList(map[string]any{"project_key": "DEMO"}); err == nil {
		t.Fatal("missing issue list accepted")
	}
}
