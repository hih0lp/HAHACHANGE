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
	for _, want := range []string{"DEMO-1", "Дата начала: не указана", "Срок: не указан", "Описание: не заполнено", "[Открыть задачу](https://jira.example/browse/DEMO-1)"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card %q lacks %q", card, want)
		}
	}
}
