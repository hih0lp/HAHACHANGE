package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatMemoryIndexesWithoutTextAndFiltersOwnerAndAge(t *testing.T) {
	const id = "f46d6713-53e7-4db9-b1a7-9a61a7224c94"
	var payload map[string]any
	var filter map[string]any
	qdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/chat-memory":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/chat-memory":
			_, _ = w.Write([]byte(`{"result":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/chat-memory/index":
			_, _ = w.Write([]byte(`{"result":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/chat-memory/points":
			var request struct {
				Points []struct {
					Payload map[string]any `json:"payload"`
				} `json:"points"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Points) != 1 {
				t.Errorf("upsert: %v", err)
				return
			}
			payload = request.Points[0].Payload
			_, _ = w.Write([]byte(`{"result":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/chat-memory/points/query":
			var request struct {
				Filter map[string]any `json:"filter"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			filter = request.Filter
			_, _ = w.Write([]byte(`{"result":{"points":[{"id":"` + id + `","score":0.91,"payload":{"owner_id":"another-owner"}},{"id":"` + id + `","score":0.88,"payload":{"owner_id":"owner-one"}}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qdrant.Close()
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Inputs []string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Inputs) != 1 {
			t.Errorf("embedding request: %v", err)
		}
		_, _ = w.Write([]byte(`[[0.2,0.8]]`))
	}))
	defer embeddings.Close()
	service := New(Config{QdrantURL: qdrant.URL, EmbeddingsURL: embeddings.URL, MemoryCollectionName: "chat-memory"})
	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if err := service.UpsertMemory(context.Background(), id, "owner-one", created, "secret conversation text"); err != nil {
		t.Fatal(err)
	}
	if payload["owner_id"] != "owner-one" || payload["created_at_ms"] == nil || strings.Contains(strings.ToLower(mustJSON(payload)), "secret") {
		t.Fatalf("unsafe memory payload: %v", payload)
	}
	hits, err := service.SearchMemory(context.Background(), "owner-one", "previous discussion", created.AddDate(0, 0, 10), 5)
	if err != nil || len(hits) != 1 || hits[0].JobID != id {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	if !strings.Contains(mustJSON(filter), "owner-one") || !strings.Contains(mustJSON(filter), "created_at_ms") {
		t.Fatalf("missing owner/time filters: %v", filter)
	}
}

func mustJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestMemoryEmbeddingInputFitsLocalModel(t *testing.T) {
	input := memoryEmbeddingInput(strings.Repeat("длинный ответ ", 100))
	if len([]rune(input)) != 300 {
		t.Fatalf("embedding input has %d runes", len([]rune(input)))
	}
}
