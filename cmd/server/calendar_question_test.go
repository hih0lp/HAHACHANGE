package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCalendarPlannerValidatesModelRange(t *testing.T) {
	response := `{"needs_events":true,"needs_availability":false,"period_kind":"current_week","time_min":"2026-11-08T00:00:00+03:00","time_max":"2026-11-15T00:00:00+03:00"}`
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, response)
	}))
	defer model.Close()
	a := answerer{url: model.URL, model: "test", client: model.Client()}
	loc, _ := time.LoadLocation("Europe/Moscow")
	plan, err := a.planCalendar(context.Background(), "Какие мероприятия на этой неделе?", time.Date(2026, 11, 12, 20, 0, 0, 0, loc))
	if err != nil || !plan.NeedsEvents || !strings.HasPrefix(plan.TimeMin, "2026-11-12T20:00") || plan.TimeMax != "2026-11-16T00:00:00+03:00" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	response = `{"needs_events":true,"time_min":"2026-11-12T00:00:00+03:00","time_max":"2027-01-01T00:00:00+03:00"}`
	if _, err := a.planCalendar(context.Background(), "Календарь", time.Now().In(loc)); err == nil {
		t.Fatal("range over 31 days accepted")
	}
}
