package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAccessDestinationReadsVerifiedFormWithExistingHistory(t *testing.T) {
	rights := mcp.NewServer(&mcp.Implementation{Name: "test-rights", Version: "1"}, nil)
	mcp.AddTool(rights, &mcp.Tool{Name: "rights_forms_list"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"forms": []formChoice{
			{Kind: "access", ID: "untrusted", URL: "https://example.org/untrusted"},
			{Kind: "access", ID: "verified", URL: "/forms/verified"},
		}}, nil
	})
	rightsHTTP := testMCPServer(t, rights)
	defer rightsHTTP.Close()
	a := answerer{rightsMCPURL: rightsHTTP.URL}
	ctx := distributed.WithTeamToken(context.Background(), "test-token")
	for _, recent := range [][]chatContextTurn{nil, {{Question: "Какие у меня права?", Answer: "Проверяю профиль."}}} {
		answer, _, err := a.answerWithContext(ctx, "Куда отправить запрос на доступ?", recent, nil)
		if err != nil || !strings.Contains(answer, "](/forms/verified)") || strings.Contains(answer, "untrusted") {
			t.Fatalf("answer=%q err=%v", answer, err)
		}
	}
	for _, other := range []string{"У меня есть доступ?", "Куда отправить запрос на доступ и какие у меня задачи?"} {
		if isAccessFormDestinationQuestion(other) {
			t.Errorf("unexpected access form match for %q", other)
		}
	}
}

func TestRepeatedQuestionIsRetriedWithoutPublishingEcho(t *testing.T) {
	for _, test := range []struct {
		name, second, want string
	}{
		{"recovered", "Я Бобби.", "Я Бобби."},
		{"repeated", "Как тебя зовут?", "Не удалось подготовить содержательный ответ на этот запрос."},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				content := "«КАК ТЕБЯ ЗОВУТ?»"
				if calls > 1 {
					content = test.second
				}
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
			}))
			defer model.Close()
			service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
			a := answerer{service: service, url: model.URL, model: "test"}
			answer, _, err := a.Ask(context.Background(), "Как тебя зовут?")
			if err != nil || answer != test.want || calls != 2 {
				t.Fatalf("answer=%q calls=%d err=%v", answer, calls, err)
			}
		})
	}
}
