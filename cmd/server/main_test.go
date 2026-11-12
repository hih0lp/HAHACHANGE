package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/rag"
)

func testHandler(t *testing.T, apiKey string) http.Handler {
	return testHandlerWithConfig(t, config{APIKey: apiKey, RAG: rag.Config{CollectionName: "playbook"}})
}

func testHandlerWithConfig(t *testing.T, cfg config) http.Handler {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rules.md"), []byte("# Правила\n\nКоманда проводит ревью."), 0o600); err != nil {
		t.Fatal(err)
	}
	created := false
	points := make([]map[string]any, 0)
	qdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/collections":
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{}})
		case r.Method == http.MethodGet && path == "/collections/playbook":
			if !created {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{}})
		case r.Method == http.MethodPut && path == "/collections/playbook":
			created = true
			json.NewEncoder(w).Encode(map[string]any{"result": true})
		case path == "/collections/playbook/points/count":
			if !created {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"count": len(points)}})
		case path == "/collections/playbook/points/delete":
			points = nil
			json.NewEncoder(w).Encode(map[string]any{"result": true})
		case r.Method == http.MethodPut && path == "/collections/playbook/points":
			var body struct {
				Points []struct {
					Payload map[string]any `json:"payload"`
				} `json:"points"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			for _, point := range body.Points {
				points = append(points, point.Payload)
			}
			json.NewEncoder(w).Encode(map[string]any{"result": true})
		case path == "/collections/playbook/points/query":
			results := make([]any, 0, len(points))
			for _, point := range points {
				results = append(results, map[string]any{"score": .9, "payload": point})
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": results}})
		default:
			http.NotFound(w, r)
		}
	}))
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Inputs []string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		vectors := make([][]float32, len(request.Inputs))
		for i := range vectors {
			vectors[i] = []float32{1, 2}
		}
		json.NewEncoder(w).Encode(vectors)
	}))
	t.Cleanup(func() { qdrant.Close(); embeddings.Close() })
	service := rag.New(rag.Config{QdrantURL: qdrant.URL, EmbeddingsURL: embeddings.URL, DocumentsPath: root, CollectionName: "playbook", ChunkSize: 80, ChunkOverlap: 10})
	if _, err := service.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	return newHandler(service, cfg)
}

func call(handler http.Handler, method, path, body, apiKey string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if apiKey != "" {
		request.Header.Set("X-API-Key", apiKey)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestKnowledgeHandlers(t *testing.T) {
	handler := testHandler(t, "secret")
	if response := call(handler, http.MethodGet, "/", "", ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Бобби · AI-buddy") {
		t.Fatalf("web page = %d", response.Code)
	}
	if response := call(handler, http.MethodPost, "/v1/access-requests", `{}`, "secret"); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("disabled access route = %d", response.Code)
	}
	if response := call(handler, http.MethodGet, "/health", "", ""); response.Code != http.StatusOK {
		t.Fatalf("health = %d", response.Code)
	}
	if response := call(handler, http.MethodGet, "/v1/knowledge/stats", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d", response.Code)
	}
	if response := call(handler, http.MethodGet, "/v1/knowledge/stats", "", "secret"); response.Code != http.StatusOK {
		t.Fatalf("stats = %d", response.Code)
	}
	if response := call(handler, http.MethodPost, "/v1/knowledge/search", "{", "secret"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", response.Code)
	}
	if response := call(handler, http.MethodPost, "/v1/knowledge/search", `{"query":"x"}`, "secret"); response.Code != http.StatusBadRequest {
		t.Fatalf("short query = %d", response.Code)
	}
	if response := call(handler, http.MethodPost, "/v1/knowledge/search", `{"query":"правила", "limit":21}`, "secret"); response.Code != http.StatusBadRequest {
		t.Fatalf("limit = %d", response.Code)
	}
	if response := call(handler, http.MethodPost, "/v1/knowledge/search", `{"query":"правила"}`, "secret"); response.Code != http.StatusOK {
		t.Fatalf("search = %d: %s", response.Code, response.Body.String())
	}
	if response := call(handler, http.MethodPost, "/v1/knowledge/reindex", "", "secret"); response.Code != http.StatusOK {
		t.Fatalf("reindex = %d", response.Code)
	}
}

func TestAskWithLLM(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if strings.Contains(request.Messages[0].Content, "Classify the user's requested evidence") {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"needs_events":false}`}}}})
			return
		}
		if request.Model == "qwen3-1.7b" {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"cleaned_question":"Как проходит ревью?","categories":["documents"]}`}}}})
			return
		}
		if request.Model != "qwen3-8b" || len(request.Messages) != 2 || request.Messages[0].Role != "system" || !strings.Contains(request.Messages[0].Content, "Тебя зовут Бобби") || request.Messages[1].Role != "user" || !strings.Contains(request.Messages[1].Content, "rules.md") {
			t.Errorf("bad LLM prompt: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "Ревью описано в rules.md"}}}})
	}))
	defer llm.Close()
	t.Setenv("LLM_URL", llm.URL)
	handler := testHandler(t, "")
	response := call(handler, http.MethodPost, "/v1/ask", `{"query":"Как проходит ревью?"}`, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "rules.md") {
		t.Fatalf("ask: %d %s", response.Code, response.Body.String())
	}
	if response := call(handler, http.MethodPost, "/v1/ask", `{"query":"x"}`, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("bad query: %d", response.Code)
	}
}

func TestConfigAndMiddlewareHelpers(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant.test")
	t.Setenv("EMBEDDINGS_URL", "http://embed.test")
	t.Setenv("DOCUMENTS_PATH", "/docs")
	t.Setenv("COLLECTION_NAME", "test")
	t.Setenv("CHUNK_SIZE", "77")
	t.Setenv("CHUNK_OVERLAP", "8")
	t.Setenv("INDEX_ON_STARTUP", "false")
	loaded := loadConfig()
	if loaded.IndexOnStartup || loaded.RAG.ChunkSize != 77 || loaded.RAG.CollectionName != "test" {
		t.Fatalf("config = %+v", loaded)
	}
	t.Setenv("BAD_BOOL", "bad")
	t.Setenv("BAD_INT", "bad")
	if !envBool("BAD_BOOL", true) || envInt("BAD_INT", 9) != 9 || env("MISSING", "fallback") != "fallback" {
		t.Fatal("environment fallbacks failed")
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if response := call(authorize("", next), http.MethodGet, "/", "", ""); response.Code != http.StatusNoContent {
		t.Fatal("empty auth should pass")
	}
	if response := call(authorize("key", next), http.MethodGet, "/", "", "key"); response.Code != http.StatusNoContent {
		t.Fatal("valid auth should pass")
	}
	if response := call(logging(next), http.MethodGet, "/", "", ""); response.Code != http.StatusNoContent {
		t.Fatal("logging should pass")
	}
	if errText("x").Error() != "x" {
		t.Fatal("error text")
	}
}

func TestHandlerDependencyFailures(t *testing.T) {
	service := rag.New(rag.Config{QdrantURL: "http://127.0.0.1:1", EmbeddingsURL: "http://127.0.0.1:1", DocumentsPath: "/missing", CollectionName: "playbook", ChunkSize: 80, ChunkOverlap: 10})
	handler := newHandler(service, config{RAG: rag.Config{CollectionName: "playbook"}})
	if response := call(handler, http.MethodGet, "/health", "", ""); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("health failure = %d", response.Code)
	}
	if response := call(handler, http.MethodGet, "/v1/knowledge/stats", "", ""); response.Code != http.StatusInternalServerError {
		t.Fatalf("stats failure = %d", response.Code)
	}
	if response := call(handler, http.MethodPost, "/v1/knowledge/reindex", "", ""); response.Code != http.StatusInternalServerError {
		t.Fatalf("reindex failure = %d", response.Code)
	}
	if response := call(handler, http.MethodPost, "/v1/knowledge/search", `{"query":"правила"}`, ""); response.Code != http.StatusInternalServerError {
		t.Fatalf("search failure = %d", response.Code)
	}
}

func TestRunBootstrapsService(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rules.md"), []byte("# Правила"), 0o600); err != nil {
		t.Fatal(err)
	}
	qdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/collections" {
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{}})
			return
		}
		if r.URL.Path == "/collections/playbook/points/count" {
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"count": 1}})
			return
		}
		http.NotFound(w, r)
	}))
	defer qdrant.Close()
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode([][]float32{{1, 2}}) }))
	defer embeddings.Close()
	t.Setenv("QDRANT_URL", qdrant.URL)
	t.Setenv("EMBEDDINGS_URL", embeddings.URL)
	t.Setenv("DOCUMENTS_PATH", root)
	t.Setenv("COLLECTION_NAME", "playbook")
	t.Setenv("INDEX_ON_STARTUP", "true")
	original := listenAndServe
	defer func() { listenAndServe = original }()
	want := errors.New("listener stopped")
	listenAndServe = func(address string, handler http.Handler) error {
		if address != ":8000" {
			t.Fatalf("address = %s", address)
		}
		return want
	}
	if err := run(); !errors.Is(err, want) {
		t.Fatalf("run error = %v", err)
	}
}

func TestRunDegradedModeServesWithoutDocumentDependencies(t *testing.T) {
	t.Setenv("ALLOW_DEGRADED_STARTUP", "true")
	t.Setenv("INDEX_ON_STARTUP", "false")
	t.Setenv("JIRA_MODE", "off")
	t.Setenv("QDRANT_URL", "http://127.0.0.1:1")
	t.Setenv("EMBEDDINGS_URL", "http://127.0.0.1:1")
	original := listenAndServe
	defer func() { listenAndServe = original }()
	want := errors.New("listener stopped")
	listenAndServe = func(address string, handler http.Handler) error {
		if address != ":8000" {
			t.Fatalf("address = %s", address)
		}
		return want
	}
	if err := run(); !errors.Is(err, want) {
		t.Fatalf("degraded startup = %v", err)
	}
}
