package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchAndBuildContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/knowledge/search" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Fatal("API key missing")
		}
		json.NewEncoder(w).Encode(map[string]any{"hits": []Hit{{Score: .9, Path: "rules.md", ChunkIndex: 1, Text: "Фрагмент"}}})
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, APIKey: "test-key"}
	hits, err := client.Search(context.Background(), "вопрос", 3)
	if err != nil || len(hits) != 1 {
		t.Fatalf("search = %+v, %v", hits, err)
	}
	context := BuildContext(hits)
	if !strings.Contains(context, "rules.md") || !strings.Contains(context, "Фрагмент") {
		t.Fatalf("bad context: %q", context)
	}
}

func TestSearchReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusUnauthorized) }))
	defer server.Close()
	_, err := (&Client{BaseURL: server.URL}).Search(context.Background(), "вопрос", 1)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchReportsInvalidURL(t *testing.T) {
	_, err := (&Client{BaseURL: "://bad"}).Search(context.Background(), "вопрос", 1)
	if err == nil {
		t.Fatal("invalid URL must fail")
	}
}
