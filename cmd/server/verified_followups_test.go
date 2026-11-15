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

func TestCommitFollowupReadsActualGitLabDiff(t *testing.T) {
	const project = "demo/repo"
	var diffRead bool
	server := mcp.NewServer(&mcp.Implementation{Name: "test-gitlab", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_projects"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"data": []gitlabProject{{ID: 2, Path: project}}}, nil
	})
	type args struct {
		Project string `json:"project"`
		ID      string `json:"id"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "gitlab_commit_diff"}, func(_ context.Context, _ *mcp.CallToolRequest, in args) (*mcp.CallToolResult, map[string]any, error) {
		diffRead = in.Project == "2" && in.ID == "01234567"
		return nil, map[string]any{"data": []map[string]string{{"old_path": "README.md", "new_path": "README.md", "diff": "+Actual change from the source\n"}}}, nil
	})
	gitlab := testMCPServer(t, server)
	defer gitlab.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Messages []struct{ Content string } }
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if len(payload.Messages) != 2 || !strings.Contains(payload.Messages[1].Content, "01234567") {
			t.Error("missing previous commit in planner context")
		}
		content := `{"needs_gitlab_projects":true,"gitlab_operation":"commit_diff","gitlab_commit":"01234567","gitlab_project":"demo/repo"}`
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer model.Close()
	a := answerer{service: rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1"}), url: model.URL, gitLabMCPURL: gitlab.URL}
	ctx := distributed.WithTeamToken(context.Background(), "session")
	answer, _, err := a.answerWithContext(ctx, "О чём речь в коммите?", []chatContextTurn{{Question: "Какие коммиты?", Answer: "demo/repo: 01234567"}}, nil)
	if err != nil || !diffRead || !strings.Contains(answer, "Actual change from the source") {
		t.Fatalf("answer=%q read=%v err=%v", answer, diffRead, err)
	}
}

func TestVerifiedOneOnOneUsesCalendarEventsAndDescribesBookingLimit(t *testing.T) {
	var data any
	err := json.Unmarshal([]byte(`{"time_min":"2026-11-15T20:00:00+03:00","time_max":"2026-11-22T20:00:00+03:00","data":{"items":[{"summary":"Окно ментора: вопросы и 1:1","start":{"dateTime":"2026-11-17T15:30:00+03:00"},"end":{"dateTime":"2026-11-17T16:00:00+03:00"}},{"summary":"Ревью задач","start":{"dateTime":"2026-11-19T16:00:00+03:00"},"end":{"dateTime":"2026-11-19T17:00:00+03:00"}}]}}`), &data)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := formatVerifiedCalendarEvents(data, "календаря наставника", true)
	if err != nil || !strings.Contains(answer, "17.11.2026 15:30–16:00") || strings.Contains(answer, "Ревью задач") || !strings.Contains(answer, "свободны ли") {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}

func TestCalendarConfirmationKeepsPreviousOneOnOneAndPeriod(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := `{"needs_events":true,"period_kind":"current_week"}`
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer model.Close()
	a := answerer{url: model.URL}
	now := time.Date(2026, 11, 15, 20, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	recent := []chatContextTurn{{Question: "Когда я доступен для 1:1 со стажёрами на неделе?"}}
	plan, err := a.planCalendar(context.Background(), "я и прошу тебя проверить в календаре", now, recent, nil)
	if err != nil || !plan.NeedsOneOnOne || plan.TimeMax != "2026-11-22T20:00:00+03:00" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestDocumentCitationsResolveOnlyReturnedFragments(t *testing.T) {
	got := insertVerifiedDocumentReferences("Источник [D1]. Второй [D2].", []rag.SearchHit{
		{Path: "playbook/readme.md", SourceURL: "https://github.com/example/playbook/blob/master/readme.md"},
	})
	if !strings.Contains(got, "[playbook/readme.md](https://github.com/example/playbook/blob/master/readme.md)") || strings.Contains(got, "[D2]") {
		t.Fatal(got)
	}
}
