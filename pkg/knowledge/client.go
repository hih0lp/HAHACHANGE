// Package knowledge is the integration point for an agent that needs RAG context.
package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

type Hit struct {
	Score      float64 `json:"score"`
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	ChunkIndex int     `json:"chunk_index"`
	Text       string  `json:"text"`
}

type searchResponse struct {
	Hits []Hit `json:"hits"`
}

func (c *Client) Search(ctx context.Context, question string, limit int) ([]Hit, error) {
	body, err := json.Marshal(map[string]any{"query": question, "limit": limit})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/knowledge/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("knowledge API returned HTTP %d: %s", resp.StatusCode, responseBody)
	}
	var result searchResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, err
	}
	return result.Hits, nil
}

// BuildContext returns cited snippets ready to append to an LLM prompt.
func BuildContext(hits []Hit) string {
	var out strings.Builder
	for _, hit := range hits {
		fmt.Fprintf(&out, "[Источник: %s, фрагмент %d]\n%s\n\n", hit.Path, hit.ChunkIndex, hit.Text)
	}
	return strings.TrimSpace(out.String())
}
