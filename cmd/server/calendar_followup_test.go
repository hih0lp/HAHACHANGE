package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCalendarFollowupUsesMoscowTimeFromTool(t *testing.T) {
	zone, _ := time.LoadLocation("Europe/Moscow")
	now := time.Now().In(zone)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, zone)
	if tomorrow.Month() != now.Month() {
		t.Skip("tomorrow is outside the current calendar month")
	}
	start := tomorrow.Add(15 * time.Hour)
	end := start.Add(20 * time.Minute)
	calendar := mcp.NewServer(&mcp.Implementation{Name: "calendar", Version: "1"}, nil)
	mcp.AddTool(calendar, &mcp.Tool{Name: "calendar_mentor_freebusy"}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		TimeMin string `json:"time_min"`
		TimeMax string `json:"time_max"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		if in.TimeMin != tomorrow.Format(time.RFC3339) || in.TimeMax != tomorrow.AddDate(0, 0, 1).Format(time.RFC3339) {
			t.Errorf("wrong Moscow day: %+v", in)
		}
		return nil, map[string]any{"time_min": in.TimeMin, "time_max": in.TimeMax, "data": map[string]any{"calendars": map[string]any{"mentor": map[string]any{"busy": []any{map[string]string{"start": start.UTC().Format(time.RFC3339), "end": end.UTC().Format(time.RFC3339)}}}}}}, nil
	})
	provider := testMCPServer(t, calendar)
	defer provider.Close()
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "Classify the current question") {
			plan := fmt.Sprintf(`{"needs_availability":true,"period_kind":"explicit","time_min":%q,"time_max":%q}`, tomorrow.Format(time.RFC3339), tomorrow.AddDate(0, 0, 1).Format(time.RFC3339))
			fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, plan)
			return
		}
		if calls == 2 {
			args, _ := json.Marshal(map[string]string{"time_min": tomorrow.Format(time.RFC3339), "time_max": tomorrow.AddDate(0, 0, 1).Format(time.RFC3339)})
			fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"calendar-1","type":"function","function":{"name":"calendar_mentor_freebusy","arguments":%q}}]}}]}`, string(args))
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Наставник занят в 12:00 UTC."}}]}`)
	}))
	defer model.Close()
	service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
	a := answerer{service: service, url: model.URL, model: "test", calendarMCPURL: provider.URL}
	answer, _, err := a.answerWithContext(distributed.WithTeamToken(context.Background(), "session"), "Завтра он свободен?", []chatContextTurn{{Question: "Когда свободен мой наставник?", Answer: "Проверю календарь."}}, nil)
	if err != nil || !strings.Contains(answer, "15:00–15:20 МСК") || strings.Contains(answer, "12:00 UTC") {
		t.Fatalf("answer %q, calls %d, error %v", answer, calls, err)
	}
}
