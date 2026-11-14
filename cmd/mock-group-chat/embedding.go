package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

type embedder struct {
	url  string
	http *http.Client
}

func (e embedder) embed(ctx context.Context, prefix string, texts []string) ([][]float32, error) {
	inputs := make([]string, len(texts))
	for i, text := range texts {
		inputs[i] = prefix + ": " + text
	}
	payload, err := json.Marshal(map[string]any{"inputs": inputs, "truncate": true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.url, "/")+"/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings returned HTTP %d", resp.StatusCode)
	}
	var vectors [][]float32
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&vectors); err != nil {
		return nil, err
	}
	if len(vectors) != len(inputs) {
		return nil, fmt.Errorf("unexpected embedding count: %d", len(vectors))
	}
	for _, vector := range vectors {
		if len(vector) != 384 {
			return nil, fmt.Errorf("expected 384 embedding dimensions, got %d", len(vector))
		}
		var squaredNorm float64
		for _, v := range vector {
			squaredNorm += float64(v) * float64(v)
		}
		if squaredNorm == 0 || math.IsNaN(squaredNorm) || math.IsInf(squaredNorm, 0) {
			return nil, fmt.Errorf("invalid embedding norm")
		}
		norm := math.Sqrt(squaredNorm)
		for i, v := range vector {
			vector[i] = float32(float64(v) / norm)
		}
	}
	return vectors, nil
}
