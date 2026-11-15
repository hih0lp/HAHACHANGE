package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
)

func TestAnswerUsesRecentChatEvenWhenOlderSearchIsEmpty(t *testing.T) {
	for _, memoryMode := range []string{"no lookup", "native", "text fallback"} {
		t.Run(memoryMode, func(t *testing.T) {
			calls, searches := 0, 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages   []map[string]any
					Tools      []map[string]any
					ToolChoice string `json:"tool_choice"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Messages) < 2 {
					t.Errorf("invalid model input: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				calls++
				if calls == 1 {
					if input.Messages[0]["content"] != chatContextDecisionPrompt {
						t.Error("context decision did not receive its own prompt")
					}
					last := input.Messages[len(input.Messages)-1]
					if last["role"] != "user" || last["content"] != "О чём говорили выше?" {
						t.Error("context decision did not receive the current question as the last message")
					}
					fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"}}]}`)
					return
				}
				context, _ := input.Messages[1]["content"].(string)
				if !strings.Contains(context, `"recent_chat":[{"question":"Что проверим?","answer":"Сверим макет."}]`) ||
					!strings.Contains(context, `"current_question":"О чём говорили выше?"`) {
					t.Error("recent history and current question were not passed to the model")
				}
				if input.ToolChoice != "auto" || len(input.Tools) != 1 {
					t.Error("memory lookup was not offered as an optional tool")
				}
				if calls == 2 {
					switch memoryMode {
					case "native":
						fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"memory-1","type":"function","function":{"name":"search_chat_memory","arguments":"{\"query\":\"макет\"}"}}]}}]}`)
						return
					case "text fallback":
						fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"tool_call\":{\"name\":\"search_chat_memory\",\"arguments\":{\"query\":\"макет\"}}}"}}]}`)
						return
					}
				} else {
					content, _ := input.Messages[len(input.Messages)-1]["content"].(string)
					if !strings.Contains(content, "no_matching_older_turns") {
						t.Error("empty search was not scoped to older messages")
					}
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"Обсуждали проверку макета."}}]}`)
			}))
			defer model.Close()
			lookup := func(_ context.Context, query string) ([]oldChatTurn, error) {
				searches++
				if query != "макет" {
					t.Errorf("unexpected lookup query: %s", query)
				}
				return nil, nil
			}
			service := rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"})
			a := answerer{service: service, url: model.URL, model: "test"}
			ctx := distributed.WithTeamToken(context.Background(), "demo-session")
			answer, _, err := a.answerWithContext(ctx, "О чём говорили выше?", []chatContextTurn{
				{Question: "Что проверим?", Answer: "Сверим макет."},
			}, lookup)
			expectedSearches := 1
			if memoryMode == "no lookup" {
				expectedSearches = 0
			}
			if err != nil || answer != "Обсуждали проверку макета." || searches != expectedSearches || calls != 2+expectedSearches {
				t.Fatalf("answer %q, calls %d, searches %d, error %v", answer, calls, searches, err)
			}
		})
	}
}
