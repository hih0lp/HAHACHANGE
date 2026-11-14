package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
)

func TestGeneralQuestionWorksWithoutDocumentSearch(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.Messages) != 2 || input.Messages[0].Role != "system" || !strings.Contains(input.Messages[0].Content, "общие вопросы") || !strings.Contains(input.Messages[1].Content, "Поиск документов временно недоступен") {
			t.Errorf("unexpected prompt: %+v", input)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Я Бобби, ваш ИИ-помощник."}}]}`))
	}))
	defer llm.Close()
	service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
	answer, sources, err := (answerer{service: service, url: llm.URL, model: "test"}).Ask(context.Background(), "Как тебя зовут?")
	if err != nil || answer != "Я Бобби, ваш ИИ-помощник." || len(sources) != 0 {
		t.Fatalf("answer=%q sources=%d err=%v", answer, len(sources), err)
	}
}

func TestGeneralConversationReceivesNoUnrelatedDocumentEvidence(t *testing.T) {
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
			return
		}
		var input struct {
			Messages []struct{ Role, Content string } `json:"messages"`
			Tools    []any                            `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.Messages) != 2 || input.Messages[1].Content != "Как дела?" || len(input.Tools) != 0 {
			t.Errorf("general question received unrelated evidence or tools: %+v", input)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Всё хорошо, спасибо! А у вас?"}}]}`))
	}))
	defer model.Close()
	service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
	a := answerer{service: service, url: model.URL, model: "test"}
	answer, sources, err := a.Ask(distributed.WithTeamToken(context.Background(), "session"), "Как дела?")
	if err != nil || answer != "Всё хорошо, спасибо! А у вас?" || len(sources) != 0 || calls != 2 {
		t.Fatalf("answer=%q sources=%d calls=%d err=%v", answer, len(sources), calls, err)
	}
}

func TestToolsForEndpointKeepsOnlyTheSelectedLiveService(t *testing.T) {
	tools := []map[string]any{
		{"type": "function", "function": map[string]any{"name": "team_members"}},
		{"type": "function", "function": map[string]any{"name": "rights_forms_list"}},
	}
	selected := toolsForEndpoint(tools, map[string]string{
		"team_members":      "http://team-mcp",
		"rights_forms_list": "http://rights-mcp",
	}, "http://team-mcp")
	if len(selected) != 1 || selected[0]["function"].(map[string]any)["name"] != "team_members" {
		t.Fatalf("selected tools = %#v", selected)
	}
}
