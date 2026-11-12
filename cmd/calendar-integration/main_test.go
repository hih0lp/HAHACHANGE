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

func TestCalendarToolsUseSelectedCalendarsAndBoundedRange(t *testing.T) {
	var seenMentor, seenFreeBusy bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.FormValue("grant_type") != "refresh_token" {
				t.Error("wrong grant")
			}
			fmt.Fprint(w, `{"access_token":"test-access"}`)
		case "/calendars/mentor/events":
			seenMentor = true
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-access" || r.URL.Query().Get("singleEvents") != "true" {
				t.Error("invalid mentor request")
			}
			fmt.Fprint(w, `{"items":[{"summary":"mentor"}]}`)
		case "/freeBusy":
			seenFreeBusy = true
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-access" {
				t.Error("invalid freebusy request")
			}
			var buf [1024]byte
			n, _ := r.Body.Read(buf[:])
			if !strings.Contains(string(buf[:n]), `"id":"mentor"`) {
				t.Error("wrong mentor calendar")
			}
			fmt.Fprint(w, `{"calendars":{"mentor":{"busy":[]}}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer provider.Close()
	b := bridge{mentorID: "mentor", clientID: "id", clientSecret: "secret", refreshToken: "refresh", tokenURL: provider.URL + "/token", apiURL: provider.URL, client: provider.Client()}
	in := query{TimeMin: "2026-11-10T09:00:00+03:00", TimeMax: "2026-11-11T18:00:00+03:00"}
	if _, err := b.call(context.Background(), "calendar_mentor_events", in); err != nil {
		t.Fatal(err)
	}
	if _, err := b.call(context.Background(), "calendar_mentor_freebusy", in); err != nil {
		t.Fatal(err)
	}
	if !seenMentor || !seenFreeBusy {
		t.Fatal("provider endpoints were not called")
	}
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"calendars":{"mentor":{"errors":[{"reason":"notFound"}]}}}`)
	}))
	defer blocked.Close()
	b.apiURL = blocked.URL
	if _, err := b.call(context.Background(), "calendar_mentor_freebusy", in); err == nil {
		t.Fatal("inaccessible mentor calendar was reported as available")
	}
	in.TimeMax = "2027-01-01T18:00:00+03:00"
	if _, err := b.call(context.Background(), "calendar_mentor_events", in); err == nil {
		t.Fatal("range over 31 days accepted")
	}
}

func TestRelativeDayUsesMoscowCalendarDate(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, moscow)
	start, end, err := resolveRange(query{Day: "tomorrow"}, now)
	if err != nil || start.Format(time.RFC3339) != "2026-11-13T00:00:00+03:00" || end.Format(time.RFC3339) != "2026-11-14T00:00:00+03:00" {
		t.Fatalf("tomorrow range: %v to %v, %v", start, end, err)
	}
	if _, _, err := resolveRange(query{Day: "tomorrow", TimeMin: now.Format(time.RFC3339)}, now); err == nil {
		t.Fatal("ambiguous day and range accepted")
	}
}
