package rag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Config struct {
	QdrantURL            string
	EmbeddingsURL        string
	DocumentsPath        string
	CollectionName       string
	MemoryCollectionName string
	ChunkSize            int
	ChunkOverlap         int
}

type Service struct {
	cfg  Config
	http *http.Client
}

type SearchHit struct {
	Score      float64 `json:"score"`
	Path       string  `json:"path"`
	SourceURL  string  `json:"source_url,omitempty"`
	Title      string  `json:"title"`
	ChunkIndex int     `json:"chunk_index"`
	Text       string  `json:"text"`
}

type IndexResult struct {
	Documents int `json:"documents"`
	Chunks    int `json:"chunks"`
}

type point struct {
	ID      string    `json:"id"`
	Vector  []float32 `json:"vector"`
	Payload any       `json:"payload,omitempty"`
}

type payload struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	ChunkIndex int    `json:"chunk_index"`
	Text       string `json:"text"`
}

func New(cfg Config) *Service {
	return &Service{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}
}

func (s *Service) WaitReady(ctx context.Context) error {
	deadline := time.Now().Add(5 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := s.qdrant(ctx, http.MethodGet, "/collections", nil, nil); err == nil {
			if _, err := s.embed(ctx, []string{"passage: readiness probe"}); err == nil {
				return nil
			} else {
				lastErr = err
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("dependencies not ready: %w", lastErr)
}

func (s *Service) IndexIfEmpty(ctx context.Context) (IndexResult, error) {
	count, err := s.Count(ctx)
	if err != nil {
		return IndexResult{}, err
	}
	if count == 0 {
		return s.Reindex(ctx)
	}
	files, err := markdownFiles(s.cfg.DocumentsPath)
	return IndexResult{Documents: len(files), Chunks: count}, err
}

func (s *Service) Reindex(ctx context.Context) (IndexResult, error) {
	files, err := markdownFiles(s.cfg.DocumentsPath)
	if err != nil {
		return IndexResult{}, err
	}

	records := make([]payload, 0)
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			return IndexResult{}, err
		}
		rel, err := filepath.Rel(s.cfg.DocumentsPath, path)
		if err != nil {
			return IndexResult{}, err
		}
		text := string(body)
		for i, chunk := range ChunkText(text, s.cfg.ChunkSize, s.cfg.ChunkOverlap) {
			records = append(records, payload{Path: filepath.ToSlash(rel), Title: title(text, filepath.Base(path)), ChunkIndex: i, Text: chunk})
		}
	}
	if len(records) == 0 {
		return IndexResult{}, errors.New("no Markdown chunks found")
	}

	inputs := make([]string, len(records))
	for i, record := range records {
		inputs[i] = "passage: " + record.Text
	}
	vectors, err := s.embed(ctx, inputs)
	if err != nil {
		return IndexResult{}, err
	}
	if err := s.ensureCollection(ctx, len(vectors[0])); err != nil {
		return IndexResult{}, err
	}
	if _, err := s.qdrant(ctx, http.MethodPost, "/collections/"+s.cfg.CollectionName+"/points/delete?wait=true", map[string]any{"filter": map[string]any{}}, nil); err != nil {
		return IndexResult{}, err
	}

	points := make([]point, len(records))
	for i, record := range records {
		points[i] = point{ID: stableID(record), Vector: vectors[i], Payload: record}
	}
	for start := 0; start < len(points); start += 64 {
		end := start + 64
		if end > len(points) {
			end = len(points)
		}
		if _, err := s.qdrant(ctx, http.MethodPut, "/collections/"+s.cfg.CollectionName+"/points?wait=true", map[string]any{"points": points[start:end]}, nil); err != nil {
			return IndexResult{}, err
		}
	}
	return IndexResult{Documents: len(files), Chunks: len(points)}, nil
}

func (s *Service) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	vectors, err := s.embed(ctx, []string{"query: " + query})
	if err != nil {
		return nil, err
	}
	var response struct {
		Result struct {
			Points []struct {
				Score   float64 `json:"score"`
				Payload payload `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	_, err = s.qdrant(ctx, http.MethodPost, "/collections/"+s.cfg.CollectionName+"/points/query", map[string]any{
		"query": vectors[0], "limit": limit, "with_payload": true,
	}, &response)
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, len(response.Result.Points))
	for i, result := range response.Result.Points {
		hits[i] = SearchHit{Score: result.Score, Path: result.Payload.Path, SourceURL: sourceURL(result.Payload.Path), Title: result.Payload.Title, ChunkIndex: result.Payload.ChunkIndex, Text: result.Payload.Text}
	}
	return hits, nil
}

func sourceURL(path string) string {
	if path == "onboarding/intern-checklist.md" {
		return "https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0/edit?usp=sharing"
	}
	if strings.HasPrefix(path, "playbook/") {
		return "https://github.com/avito-tech/playbook/blob/master/" + strings.TrimPrefix(path, "playbook/")
	}
	return ""
}

func (s *Service) Count(ctx context.Context) (int, error) {
	var response struct {
		Result struct {
			Count int `json:"count"`
		} `json:"result"`
	}
	_, err := s.qdrant(ctx, http.MethodPost, "/collections/"+s.cfg.CollectionName+"/points/count", map[string]any{"exact": true}, &response)
	if err != nil {
		// A missing collection is an empty collection before the first index.
		if strings.Contains(err.Error(), "404") {
			return 0, nil
		}
		return 0, err
	}
	return response.Result.Count, nil
}

func (s *Service) ensureCollection(ctx context.Context, vectorSize int) error {
	_, err := s.qdrant(ctx, http.MethodGet, "/collections/"+s.cfg.CollectionName, nil, nil)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "404") {
		return err
	}
	_, err = s.qdrant(ctx, http.MethodPut, "/collections/"+s.cfg.CollectionName, map[string]any{
		"vectors": map[string]any{"size": vectorSize, "distance": "Cosine"},
	}, nil)
	return err
}

func (s *Service) embed(ctx context.Context, inputs []string) ([][]float32, error) {
	const batchSize = 16 // Below TEI's max-client-batch-size (64).
	vectors := make([][]float32, 0, len(inputs))
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch, err := s.embedBatch(ctx, inputs[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func (s *Service) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	// TEI's /embed endpoint expects an object with an inputs field. Keeping the
	// wire format here (instead of in handlers) also makes the agent API stable.
	request, err := json.Marshal(map[string][]string{"inputs": inputs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.cfg.EmbeddingsURL, "/")+"/embed", bytes.NewReader(request))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embeddings returned HTTP %d: %s", resp.StatusCode, body)
	}
	var vectors [][]float32
	if err := json.Unmarshal(body, &vectors); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	if len(vectors) != len(inputs) || len(vectors) == 0 {
		return nil, errors.New("embeddings returned an unexpected number of vectors")
	}
	return vectors, nil
}

func (s *Service) qdrant(ctx context.Context, method, path string, input any, output any) ([]byte, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.cfg.QdrantURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return response, fmt.Errorf("qdrant %s %s returned HTTP %d: %s", method, path, resp.StatusCode, response)
	}
	if output != nil && len(response) > 0 {
		if err := json.Unmarshal(response, output); err != nil {
			return response, err
		}
	}
	return response, nil
}

func markdownFiles(root string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func title(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			if heading := strings.TrimSpace(strings.TrimLeft(line, "#")); heading != "" {
				return heading
			}
		}
	}
	return strings.TrimSuffix(fallback, filepath.Ext(fallback))
}

func stableID(record payload) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", record.Path, record.ChunkIndex, record.Text)))
	encoded := hex.EncodeToString(sum[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}
