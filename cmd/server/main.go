package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hackchange/avito-rag/internal/rag"
)

type config struct {
	APIKey         string
	IndexOnStartup bool
	RAG            rag.Config
	Jira           jiraConfig
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

var listenAndServe = http.ListenAndServe

func run() error {
	cfg := loadConfig()
	if err := cfg.Jira.validate(); err != nil {
		return err
	}
	service := rag.New(cfg.RAG)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := service.WaitReady(ctx); err != nil {
		return fmt.Errorf("dependencies are not ready: %w", err)
	}
	if cfg.IndexOnStartup {
		result, err := service.IndexIfEmpty(ctx)
		if err != nil {
			return fmt.Errorf("initial indexing failed: %w", err)
		}
		log.Printf("knowledge base ready: %d documents, %d chunks", result.Documents, result.Chunks)
	}

	log.Printf("knowledge API listens on :8000")
	return listenAndServe(":8000", logging(newHandler(service, cfg)))
}

func newHandler(service *rag.Service, cfg config) http.Handler {
	mux := http.NewServeMux()
	answer := answerer{service: service, url: env("LLM_URL", "http://host.docker.internal:1234"), model: env("LLM_MODEL", "qwen3-8b"), mcpURL: env("MCP_URL", "http://127.0.0.1:8000/mcp"), mcpKey: cfg.APIKey, jiraMode: cfg.Jira.Mode}
	jobs := newJobManager(envInt("JOB_WORKERS", 2), envInt("JOB_QUEUE_SIZE", 32), time.Duration(envInt("JOB_TIMEOUT_SECONDS", 180))*time.Second, answer.Ask)
	registerJobRoutes(mux, jobs, cfg.APIKey)
	registerAccessRoutes(mux, cfg.APIKey, cfg.Jira)
	mcpHandler := authorize(cfg.APIKey, newJiraMCP(cfg.Jira))
	mux.Handle("POST /mcp", mcpHandler)
	mux.Handle("GET /mcp", mcpHandler)
	mux.Handle("DELETE /mcp", mcpHandler)
	mux.HandleFunc("GET /", webPage)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		count, err := service.Count(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "collection": cfg.RAG.CollectionName, "chunks": count})
	})
	mux.Handle("GET /v1/knowledge/stats", authorize(cfg.APIKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, err := service.IndexIfEmpty(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})))
	mux.Handle("POST /v1/knowledge/reindex", authorize(cfg.APIKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, err := service.Reindex(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})))
	mux.Handle("POST /v1/knowledge/search", authorize(cfg.APIKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		input.Query = strings.TrimSpace(input.Query)
		if len([]rune(input.Query)) < 2 {
			writeError(w, http.StatusBadRequest, errText("query must be at least 2 characters"))
			return
		}
		if input.Limit == 0 {
			input.Limit = 5
		}
		if input.Limit < 1 || input.Limit > 20 {
			writeError(w, http.StatusBadRequest, errText("limit must be between 1 and 20"))
			return
		}
		hits, err := service.Search(r.Context(), input.Query, input.Limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
	})))
	mux.Handle("POST /v1/ask", authorize(cfg.APIKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		input.Query = strings.TrimSpace(input.Query)
		if len([]rune(input.Query)) < 2 {
			writeError(w, http.StatusBadRequest, errText("query must be at least 2 characters"))
			return
		}
		answer, _, err := answer.Ask(r.Context(), input.Query)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"answer": answer})
	})))

	return mux
}

func loadConfig() config {
	return config{
		APIKey:         os.Getenv("API_KEY"),
		IndexOnStartup: envBool("INDEX_ON_STARTUP", true),
		Jira:           jiraConfig{Mode: env("JIRA_MODE", "off"), BaseURL: env("JIRA_BASE_URL", "https://gapshinmihael.atlassian.net"), ProjectKey: env("JIRA_PROJECT_KEY", "SCRUM"), IssueType: env("JIRA_ISSUE_TYPE", "Задача"), Email: os.Getenv("JIRA_EMAIL"), APIToken: os.Getenv("JIRA_API_TOKEN"), DataPath: env("JIRA_DATA_PATH", "/data/jira/access-requests.json")},
		RAG: rag.Config{
			QdrantURL:      env("QDRANT_URL", "http://qdrant:6333"),
			EmbeddingsURL:  env("EMBEDDINGS_URL", "http://embeddings:80"),
			DocumentsPath:  env("DOCUMENTS_PATH", "/data/playbook"),
			CollectionName: env("COLLECTION_NAME", "avito_playbook"),
			ChunkSize:      envInt("CHUNK_SIZE", 1100),
			ChunkOverlap:   envInt("CHUNK_OVERLAP", 180),
		},
	}
}

func authorize(key string, next http.Handler) http.Handler {
	if key == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != key {
			writeError(w, http.StatusUnauthorized, errText("invalid API key"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started))
	})
}
func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

type errText string

func (e errText) Error() string { return string(e) }
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
