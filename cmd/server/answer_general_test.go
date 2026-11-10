package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
