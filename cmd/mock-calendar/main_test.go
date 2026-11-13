package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEditableCalendarAndRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.json")
	write := func(day string) {
		t.Helper()
		raw := `{"calendar_id":"mentor","time_zone":"Europe/Moscow","events":[{"id":"slot","summary":"1:1","start":{"dateTime":"2026-11-` + day + `T15:30:00+03:00"},"end":{"dateTime":"2026-11-` + day + `T16:00:00+03:00"}}]}`
		if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	a, _ := time.Parse(time.RFC3339, "2026-11-17T00:00:00+03:00")
	b := a.Add(24 * time.Hour)
	write("17")
	f, e := load(path)
	if e != nil || len(selectEvents(f, a, b)) != 1 {
		t.Fatalf("%+v %v", f, e)
	}
	write("19")
	f, e = load(path)
	if e != nil || len(selectEvents(f, a, b)) != 0 {
		t.Fatalf("%+v %v", f, e)
	}
	if _, _, e = rangeQuery(query{TimeMin: "bad", TimeMax: b.Format(time.RFC3339)}); e == nil {
		t.Fatal("invalid range accepted")
	}
}
