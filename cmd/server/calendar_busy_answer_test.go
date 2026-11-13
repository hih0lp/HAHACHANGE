package main

import (
	"strings"
	"testing"
)

func TestFormatCalendarBusyUsesOnlyReturnedIntervals(t *testing.T) {
	result := map[string]any{"data": map[string]any{"calendars": map[string]any{"mentor": map[string]any{"busy": []any{map[string]string{"start": "2026-11-19T13:00:00Z", "end": "2026-11-19T14:00:00Z"}}}}}}
	answer, err := formatCalendarBusy(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "19.11.2026 16:00–17:00 МСК") || strings.Contains(answer, "13:00") {
		t.Fatalf("unexpected busy answer: %q", answer)
	}
}

func TestFormatCalendarBusyRejectsMissingData(t *testing.T) {
	if _, err := formatCalendarBusy(map[string]any{"data": map[string]any{}}); err == nil {
		t.Fatal("missing calendar data must fail")
	}
}

func TestFormatCalendarEventsUsesReturnedTimes(t *testing.T) {
	result := map[string]any{"data": map[string]any{"items": []any{map[string]any{"summary": "Ревью", "description": "<p>Обсуждение &amp; разбор</p>", "htmlLink": "https://www.google.com/calendar/event?eid=demo", "start": map[string]string{"dateTime": "2026-11-19T13:00:00Z"}, "end": map[string]string{"dateTime": "2026-11-19T14:00:00Z"}}}}}
	answer, err := formatCalendarEvents(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "19.11.2026 16:00–17:00 МСК — Ревью. Обсуждение & разбор") || strings.Contains(answer, "13:00") || !strings.Contains(answer, "[Открыть событие](https://www.google.com/calendar/event?eid=demo)") {
		t.Fatalf("unexpected event answer: %q", answer)
	}
}
