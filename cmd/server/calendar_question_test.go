package main

import (
	"testing"
	"time"
)

func TestCalendarQuestionRangeMoscow(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, loc)
	for _, question := range []string{"Что на завтра в онбординге?", "Какие события на 13 ноября 2026?"} {
		start, end, ok := calendarQuestionRange(question, now)
		if !ok || start.Format(time.RFC3339) != "2026-11-13T00:00:00+03:00" || end.Format(time.RFC3339) != "2026-11-14T00:00:00+03:00" {
			t.Fatalf("%q: %v %v %v", question, start, end, ok)
		}
	}
}
