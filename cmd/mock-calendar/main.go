package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hackchange/avito-rag/internal/mockfixture"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type event struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Start       struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone,omitempty"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone,omitempty"`
	} `json:"end"`
	Status string `json:"status,omitempty"`
}
type fixture struct {
	CalendarID string  `json:"calendar_id"`
	Summary    string  `json:"summary"`
	TimeZone   string  `json:"time_zone"`
	Events     []event `json:"events"`
}
type query struct {
	TimeMin string `json:"time_min,omitempty"`
	TimeMax string `json:"time_max,omitempty"`
}

func load(path string) (fixture, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return fixture{}, e
	}
	return parseFixture(b)
}

func parseFixture(b []byte) (fixture, error) {
	var f fixture
	if e := json.Unmarshal(b, &f); e != nil {
		return f, e
	}
	if f.CalendarID == "" || f.TimeZone == "" {
		return f, errors.New("calendar_id and time_zone required")
	}
	ids := map[string]bool{}
	for _, v := range f.Events {
		s, a := time.Parse(time.RFC3339, v.Start.DateTime)
		end, b := time.Parse(time.RFC3339, v.End.DateTime)
		if v.ID == "" || v.Summary == "" || a != nil || b != nil || !end.After(s) || ids[v.ID] {
			return f, fmt.Errorf("invalid or duplicate event %q", v.ID)
		}
		ids[v.ID] = true
	}
	return f, nil
}
func validateCalendarFixture(data []byte) error {
	f, err := parseFixture(data)
	if err == nil && f.Events == nil {
		return errors.New("Требуется список событий календаря.")
	}
	return err
}

func readCalendar(ctx context.Context, path string, dataset *mockfixture.Dataset) (fixture, error) {
	if dataset == nil {
		return load(path)
	}
	data, err := dataset.Bytes(ctx)
	if err != nil {
		return fixture{}, err
	}
	return parseFixture(data)
}

func rangeQuery(q query) (time.Time, time.Time, error) {
	a, e := time.Parse(time.RFC3339, q.TimeMin)
	if e != nil {
		return a, a, errors.New("time_min must be RFC3339")
	}
	b, e := time.Parse(time.RFC3339, q.TimeMax)
	if e != nil || !b.After(a) || b.Sub(a) > 31*24*time.Hour {
		return a, b, errors.New("range must be positive and at most 31 days")
	}
	return a, b, nil
}
func selectEvents(f fixture, a, b time.Time) []event {
	out := []event{}
	for _, v := range f.Events {
		start, _ := time.Parse(time.RFC3339, v.Start.DateTime)
		end, _ := time.Parse(time.RFC3339, v.End.DateTime)
		if v.Status != "cancelled" && (b.IsZero() || start.Before(b) && end.After(a)) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.DateTime < out[j].Start.DateTime })
	return out
}
func main() {
	path := os.Getenv("MOCK_CALENDAR_JSON")
	if path == "" {
		log.Fatal("MOCK_CALENDAR_JSON is required")
	}
	directory := os.Getenv("TEAM_DIRECTORY_URL")
	subjects := calendarSubjects(os.Getenv("CALENDAR_DEMO_SUBJECT"))
	companyPath := os.Getenv("MOCK_COMPANY_CALENDAR_JSON")
	if directory == "" || len(subjects) == 0 {
		log.Fatal("team directory binding required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := mockfixture.OpenDatabase(ctx, os.Getenv("MOCK_DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	if db != nil {
		defer db.Close()
	}
	mentor, err := mockfixture.Seed(ctx, db, "calendar", func() ([]byte, error) { return os.ReadFile(path) }, validateCalendarFixture)
	if err != nil {
		log.Fatal(err)
	}
	var company *mockfixture.Dataset
	if companyPath != "" {
		company, err = mockfixture.Seed(ctx, db, "company-calendar", func() ([]byte, error) { return os.ReadFile(companyPath) }, validateCalendarFixture)
		if err != nil {
			log.Fatal(err)
		}
	}
	cancel()
	server := mcp.NewServer(&mcp.Implementation{Name: "bobby-mock-calendar", Version: "1.0.0"}, nil)
	if companyPath != "" {
		mcp.AddTool(server, &mcp.Tool{Name: "calendar_company_events", Description: "List confirmed company events for the requested RFC3339 period."}, func(ctx context.Context, _ *mcp.CallToolRequest, q query) (*mcp.CallToolResult, map[string]any, error) {
			start, end, err := rangeQuery(q)
			if err != nil {
				return nil, nil, err
			}
			f, err := readCalendar(ctx, companyPath, company)
			if err != nil {
				return nil, nil, err
			}
			data := map[string]any{"kind": "calendar#events", "summary": f.Summary, "timeZone": f.TimeZone, "items": selectEvents(f, start, end)}
			return nil, map[string]any{"data": data, "source": "Демо-календарь компании", "time_min": q.TimeMin, "time_max": q.TimeMax}, nil
		})
	}
	for _, tool := range []struct{ name, description string }{{"calendar_onboarding_events", "Fetch scheduled events from the mentor calendar for a bounded RFC3339 range."}, {"calendar_mentor_freebusy", "Fetch busy intervals from the same mentor calendar; does not identify event titles."}} {
		tool := tool
		mcp.AddTool(server, &mcp.Tool{Name: tool.name, Description: tool.description}, func(ctx context.Context, _ *mcp.CallToolRequest, q query) (*mcp.CallToolResult, map[string]any, error) {
			var a, b time.Time
			if tool.name != "calendar_onboarding_events" || q.TimeMin != "" || q.TimeMax != "" {
				var err error
				a, b, err = rangeQuery(q)
				if err != nil {
					return nil, nil, err
				}
			}
			f, e := readCalendar(ctx, path, mentor)
			if e != nil {
				return nil, nil, e
			}
			events := selectEvents(f, a, b)
			var data any
			if tool.name == "calendar_onboarding_events" {
				data = map[string]any{"kind": "calendar#events", "summary": f.Summary, "timeZone": f.TimeZone, "items": events}
			} else {
				busy := []map[string]string{}
				for _, v := range events {
					s, _ := time.Parse(time.RFC3339, v.Start.DateTime)
					end, _ := time.Parse(time.RFC3339, v.End.DateTime)
					busy = append(busy, map[string]string{"start": s.UTC().Format(time.RFC3339), "end": end.UTC().Format(time.RFC3339)})
				}
				data = map[string]any{"kind": "calendar#freeBusy", "timeMin": a.UTC().Format(time.RFC3339Nano), "timeMax": b.UTC().Format(time.RFC3339Nano), "calendars": map[string]any{f.CalendarID: map[string]any{"busy": busy}}}
			}
			return nil, map[string]any{"data": data, "source": "Google Calendar demo", "time_min": q.TimeMin, "time_max": q.TimeMax}, nil
		})
	}
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if _, err := readCalendar(r.Context(), path, mentor); err != nil {
			http.Error(w, "calendar data unavailable", http.StatusServiceUnavailable)
			return
		}
		if companyPath != "" {
			if _, err := readCalendar(r.Context(), companyPath, company); err != nil {
				http.Error(w, "company calendar data unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if mentor != nil {
		team := scenario.Client{BaseURL: directory}
		mentor.Register(mux, team)
		if company != nil {
			company.Register(mux, team)
		}
	} else {
		mockfixture.Register(mux, path, "calendar_save_fixture", os.Getenv("MOCK_FIXTURE_WRITE_TOKEN"), validateCalendarFixture)
	}
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) < 8 || token[:7] != "Bearer " {
			http.Error(w, "team authorization required", 403)
			return
		}
		req, e := http.NewRequestWithContext(r.Context(), "GET", directory+"/v1/me", nil)
		if e != nil {
			http.Error(w, "team unavailable", 503)
			return
		}
		req.Header.Set("Authorization", token)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			http.Error(w, "team unavailable", 503)
			return
		}
		defer resp.Body.Close()
		var p struct {
			ID string `json:"id"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&p) != nil || !subjects[p.ID] {
			http.Error(w, "calendar binding unavailable", 403)
			return
		}
		stream.ServeHTTP(w, r)
	}))
	listen := os.Getenv("MOCK_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	log.Fatal(http.ListenAndServe(listen, mux))
}

func calendarSubjects(value string) map[string]bool {
	subjects := map[string]bool{}
	for _, subject := range strings.Split(value, ",") {
		if subject = strings.TrimSpace(subject); subject != "" {
			subjects[subject] = true
		}
	}
	return subjects
}
