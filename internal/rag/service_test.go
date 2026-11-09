package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type qdrantStub struct {
	mu         sync.Mutex
	collection bool
	points     []payload
}

func newRAGTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "design.md"), []byte("# Ревью дизайна\n\nТехнический дизайн проходит обязательное ревью команды."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "okr.md"), []byte("# Целеполагание\n\nOKR помогают командам синхронизировать цели."), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &qdrantStub{}
	qdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store.mu.Lock()
		defer store.mu.Unlock()
		path := r.URL.Path
		if r.Method == http.MethodGet && path == "/collections" {
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"collections": []any{}}})
			return
		}
		if path == "/collections/playbook" && r.Method == http.MethodGet {
			if !store.collection {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{}})
			return
		}
		if path == "/collections/playbook" && r.Method == http.MethodPut {
			store.collection = true
			json.NewEncoder(w).Encode(map[string]any{"result": true})
			return
		}
		if path == "/collections/playbook/points/count" {
			if !store.collection {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"count": len(store.points)}})
			return
		}
		if path == "/collections/playbook/points/delete" {
			store.points = nil
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{}})
			return
		}
		if path == "/collections/playbook/points" && r.Method == http.MethodPut {
			var request struct {
				Points []struct {
					Payload payload `json:"payload"`
				} `json:"points"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode upsert: %v", err)
				http.Error(w, err.Error(), 400)
				return
			}
			for _, point := range request.Points {
				store.points = append(store.points, point.Payload)
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{}})
			return
		}
		if path == "/collections/playbook/points/query" {
			results := make([]any, 0, len(store.points))
			for i, point := range store.points {
				results = append(results, map[string]any{"score": 1.0 - float64(i)/10, "payload": point})
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": results}})
			return
		}
		http.NotFound(w, r)
	}))
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Inputs []string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		vectors := make([][]float32, len(request.Inputs))
		for i, input := range request.Inputs {
			vectors[i] = []float32{float32(len(input)), 1}
		}
		json.NewEncoder(w).Encode(vectors)
	}))

	service := New(Config{QdrantURL: qdrant.URL, EmbeddingsURL: embeddings.URL, DocumentsPath: root, CollectionName: "playbook", ChunkSize: 40, ChunkOverlap: 8})
	return service, func() { qdrant.Close(); embeddings.Close() }
}

func TestServiceIndexesAndSearches(t *testing.T) {
	service, closeAll := newRAGTestService(t)
	defer closeAll()
	ctx := context.Background()
	if err := service.WaitReady(ctx); err != nil {
		t.Fatalf("wait ready: %v", err)
	}
	result, err := service.IndexIfEmpty(ctx)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if result.Documents != 2 || result.Chunks < 2 {
		t.Fatalf("unexpected index result: %+v", result)
	}
	count, err := service.Count(ctx)
	if err != nil || count != result.Chunks {
		t.Fatalf("count = %d, %v", count, err)
	}
	hits, err := service.Search(ctx, "Как проходит ревью?", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != result.Chunks || !strings.HasSuffix(hits[0].Path, ".md") {
		t.Fatalf("unexpected hits: %+v", hits)
	}
	rebuilt, err := service.Reindex(ctx)
	if err != nil || rebuilt.Chunks != result.Chunks {
		t.Fatalf("reindex = %+v, %v", rebuilt, err)
	}
}

func TestHelpers(t *testing.T) {
	if got := title("# Заголовок\nтекст", "fallback.md"); got != "Заголовок" {
		t.Fatalf("title = %q", got)
	}
	if got := title("текст", "fallback.md"); got != "fallback" {
		t.Fatalf("fallback title = %q", got)
	}
	if stableID(payload{Path: "a.md", ChunkIndex: 1, Text: "x"}) != stableID(payload{Path: "a.md", ChunkIndex: 1, Text: "x"}) {
		t.Fatal("stable id changed")
	}
	if _, err := markdownFiles("/path/that/does/not/exist"); err == nil {
		t.Fatal("missing directory must fail")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "ignored.md"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kept.MD"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := markdownFiles(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("markdown files = %v, %v", files, err)
	}
	if got := title("#\ntext", "fallback.md"); got != "fallback" {
		t.Fatalf("empty heading title = %q", got)
	}
}

func TestServiceFailurePaths(t *testing.T) {
	badQdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collections":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "/collections/playbook":
			http.Error(w, "broken", http.StatusInternalServerError)
		default:
			http.Error(w, "broken", http.StatusInternalServerError)
		}
	}))
	defer badQdrant.Close()
	badEmbed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "broken", http.StatusServiceUnavailable) }))
	defer badEmbed.Close()
	service := New(Config{QdrantURL: badQdrant.URL, EmbeddingsURL: badEmbed.URL, DocumentsPath: t.TempDir(), CollectionName: "playbook", ChunkSize: 20, ChunkOverlap: 3})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.WaitReady(cancelled); err == nil {
		t.Fatal("cancelled readiness must fail")
	}
	if _, err := service.embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("failed embed must return an error")
	}
	if _, err := service.qdrant(context.Background(), http.MethodGet, "/collections", nil, nil); err == nil {
		t.Fatal("failed qdrant request must return an error")
	}
	if err := service.ensureCollection(context.Background(), 2); err == nil {
		t.Fatal("failed collection check must return an error")
	}
	if _, err := service.Count(context.Background()); err == nil {
		t.Fatal("failed count must return an error")
	}
	if _, err := service.Reindex(context.Background()); err == nil {
		t.Fatal("empty documents must return an error")
	}
	if _, err := service.Search(context.Background(), "query", 1); err == nil {
		t.Fatal("search with bad embedding must return an error")
	}
	invalidURL := New(Config{EmbeddingsURL: "://not-a-url"})
	if _, err := invalidURL.embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("invalid embedding URL must fail")
	}
	invalidQdrant := New(Config{QdrantURL: "://not-a-url"})
	if _, err := invalidQdrant.qdrant(context.Background(), http.MethodGet, "/", nil, nil); err == nil {
		t.Fatal("invalid qdrant URL must fail")
	}
}

func TestMalformedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("not-json")) }))
	defer server.Close()
	service := New(Config{QdrantURL: server.URL, EmbeddingsURL: server.URL})
	if _, err := service.embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("malformed embeddings must fail")
	}
	var output map[string]any
	if _, err := service.qdrant(context.Background(), http.MethodGet, "/", nil, &output); err == nil {
		t.Fatal("malformed qdrant JSON must fail")
	}
}

func TestEmbeddingsAreBatched(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Inputs []string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Inputs) > 16 {
			t.Errorf("batch too large: %d", len(request.Inputs))
		}
		calls++
		vectors := make([][]float32, len(request.Inputs))
		for i := range vectors {
			vectors[i] = []float32{1, 2}
		}
		_ = json.NewEncoder(w).Encode(vectors)
	}))
	defer server.Close()
	inputs := make([]string, 33)
	for i := range inputs {
		inputs[i] = "passage: test"
	}
	vectors, err := New(Config{EmbeddingsURL: server.URL}).embed(context.Background(), inputs)
	if err != nil || len(vectors) != 33 || calls != 3 {
		t.Fatalf("vectors=%d calls=%d err=%v", len(vectors), calls, err)
	}
}

func TestSourceLinks(t *testing.T) {
	if got := sourceURL("onboarding/intern-checklist.md"); got != "https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0/edit?usp=sharing" {
		t.Fatalf("checklist URL = %s", got)
	}
	if got := sourceURL("playbook/students.md"); got != "https://github.com/avito-tech/playbook/blob/master/students.md" {
		t.Fatalf("playbook URL = %s", got)
	}
	if got := sourceURL("unknown.md"); got != "" {
		t.Fatalf("unknown URL = %s", got)
	}
}
