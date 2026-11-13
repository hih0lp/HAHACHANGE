package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/hackchange/avito-rag/internal/scenario"
)

// Run only in an isolated one-shot container on the MCP network. This test
// reads demo credentials from its environment and never records them.
func TestLiveRouteCases(t *testing.T) {
	if os.Getenv("BOBBY_LIVE_EVAL") != "1" {
		t.Skip("requires live MCP network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	login, err := (scenario.Client{BaseURL: os.Getenv("TEAM_ACCESS_URL")}).Login(ctx, "intern", os.Getenv("TEAM_ACCESS_INTERN_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	collection := os.Getenv("COLLECTION_NAME")
	if collection == "" {
		collection = "avito_playbook"
	}
	service := rag.New(rag.Config{QdrantURL: os.Getenv("QDRANT_URL"), EmbeddingsURL: os.Getenv("EMBEDDINGS_URL"), CollectionName: collection})
	a := answerer{service: service, url: os.Getenv("LLM_URL"), model: "qwen3-8b", routerModel: "qwen3-1.7b", mcpURL: os.Getenv("MCP_URL"), jiraMode: "live", teamMCPURL: os.Getenv("TEAM_MCP_URL"), rightsMCPURL: os.Getenv("RIGHTS_MCP_URL"), gitLabMCPURL: os.Getenv("GITLAB_MCP_URL"), calendarMCPURL: os.Getenv("CALENDAR_MCP_URL"), client: &http.Client{Timeout: 3 * time.Minute}}
	cases := []struct{ name, question, want string }{
		{"jira", "Для SCRUM-7 скажи статус, срок исполнения и дату начала", "SCRUM-7"},
		{"personal", "Какие у меня задачи в Jira?", "SCRUM-7"},
		{"calendar", "Какие мероприятия завтра в онбординге?", "14.11.2026"},
		{"rights", "Хочу получить доступ к джире, куда заявку отправить?", "/forms/access-request"},
		{"team", "Кто мой наставник?", "Наставник"},
		{"gitlab", "Какие открытые MR есть в моих проектах GitLab?", "MR"},
		{"documents", "Что в документации сказано про ревью кода?", "ревью"},
		{"recommendation", "Что мне подучить с учетом моих задач Jira, MR GitLab, команды и документации?", ""},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			requestCtx, stop := context.WithTimeout(distributed.WithVerifiedTeamToken(context.Background(), login.AccessToken), 3*time.Minute)
			defer stop()
			started := time.Now()
			answer, _, err := a.Ask(requestCtx, item.question)
			t.Logf("duration=%s answer=%q", time.Since(started), answer)
			if err != nil {
				t.Fatal(err)
			}
			if item.want != "" && !strings.Contains(strings.ToLower(answer), strings.ToLower(item.want)) {
				t.Fatalf("missing %q", item.want)
			}
			if item.name == "recommendation" && strings.Contains(answer, "Не удалось проверить обоснование") {
				t.Fatal("recommendation response failed source validation")
			}
		})
	}
}
