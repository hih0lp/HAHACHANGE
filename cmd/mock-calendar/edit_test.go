package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateEventDates(t *testing.T) {
	r := httptest.NewRequest("PUT", "/v1/mock-data/calendar/events/slot", nil)
	r.SetPathValue("id", "slot")
	data := []byte(`{"calendar_id":"mentor","time_zone":"Europe/Moscow","events":[{"id":"slot","summary":"Meeting","start":{"dateTime":"2026-11-16T12:00:00+03:00"},"end":{"dateTime":"2026-11-16T12:20:00+03:00"}}]}`)
	changed, err := updateEvent(data, r, []byte(`{"start":"2026-11-17T15:00:00+03:00","end":"2026-11-17T15:20:00+03:00"}`))
	if err != nil || validateCalendarFixture(changed) != nil || !strings.Contains(string(changed), "2026-11-17T15:00:00+03:00") {
		t.Fatalf("changed: %s %v", changed, err)
	}
	invalid, err := updateEvent(data, r, []byte(`{"start":"bad"}`))
	if err == nil && validateCalendarFixture(invalid) == nil {
		t.Fatal("bad date accepted")
	}
}
