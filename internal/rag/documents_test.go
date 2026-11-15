package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindDocumentByPathRejectsMismatchedSearchResult(t *testing.T) {
	path := "playbook/readme.md"
	returned := path
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Filter map[string]any `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || r.URL.Path != "/collections/playbook/points/scroll" {
			t.Errorf("scroll request: %s %v", r.URL.Path, err)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": []any{map[string]any{"payload": map[string]any{"path": returned, "title": "Playbook", "text": "Index", "chunk_index": 0}}}}})
	}))
	defer server.Close()
	service := New(Config{QdrantURL: server.URL, CollectionName: "playbook"})
	hit, found, err := service.FindDocumentByPath(context.Background(), path)
	if err != nil || !found || hit.Path != path || hit.Title != "Playbook" {
		t.Fatalf("document=%+v found=%v err=%v", hit, found, err)
	}
	returned = "other.md"
	if _, found, err := service.FindDocumentByPath(context.Background(), path); err != nil || found {
		t.Fatalf("mismatched document found=%v err=%v", found, err)
	}
}
