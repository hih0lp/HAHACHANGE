package rag

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type MemoryHit struct {
	JobID string
	Score float64
}

func (s *Service) UpsertMemory(ctx context.Context, jobID, owner string, createdAt time.Time, text string) error {
	if s.cfg.MemoryCollectionName == "" || jobID == "" || owner == "" || strings.TrimSpace(text) == "" {
		return errors.New("chat memory is not configured or its record is incomplete")
	}
	vectors, err := s.embed(ctx, []string{"passage: " + text})
	if err != nil {
		return err
	}
	if err := s.ensureMemoryCollection(ctx, len(vectors[0])); err != nil {
		return err
	}
	_, err = s.qdrant(ctx, http.MethodPut, "/collections/"+s.cfg.MemoryCollectionName+"/points?wait=true", map[string]any{
		"points": []point{{ID: jobID, Vector: vectors[0], Payload: map[string]any{"owner_id": owner, "created_at_ms": createdAt.UnixMilli()}}},
	}, nil)
	return err
}

func (s *Service) SearchMemory(ctx context.Context, owner, query string, before time.Time, limit int) ([]MemoryHit, error) {
	if s.cfg.MemoryCollectionName == "" || owner == "" || limit < 1 {
		return nil, errors.New("chat memory search is not configured")
	}
	vectors, err := s.embed(ctx, []string{"query: " + query})
	if err != nil {
		return nil, err
	}
	var response struct {
		Result struct {
			Points []struct {
				ID      json.RawMessage `json:"id"`
				Score   float64         `json:"score"`
				Payload struct {
					Owner string `json:"owner_id"`
				} `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	_, err = s.qdrant(ctx, http.MethodPost, "/collections/"+s.cfg.MemoryCollectionName+"/points/query", map[string]any{
		"query": vectors[0], "limit": limit, "with_payload": true,
		"filter": map[string]any{"must": []any{
			map[string]any{"key": "owner_id", "match": map[string]string{"value": owner}},
			map[string]any{"key": "created_at_ms", "range": map[string]int64{"lt": before.UnixMilli()}},
		}},
	}, &response)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	hits := make([]MemoryHit, 0, len(response.Result.Points))
	for _, item := range response.Result.Points {
		var id string
		if json.Unmarshal(item.ID, &id) == nil && item.Payload.Owner == owner && id != "" {
			hits = append(hits, MemoryHit{JobID: id, Score: item.Score})
		}
	}
	return hits, nil
}

func (s *Service) ensureMemoryCollection(ctx context.Context, size int) error {
	name := s.cfg.MemoryCollectionName
	if _, err := s.qdrant(ctx, http.MethodGet, "/collections/"+name, nil, nil); err != nil && !strings.Contains(err.Error(), "404") {
		return err
	} else if err != nil {
		if _, err := s.qdrant(ctx, http.MethodPut, "/collections/"+name, map[string]any{
			"vectors": map[string]any{"size": size, "distance": "Cosine"},
		}, nil); err != nil && !strings.Contains(err.Error(), "409") {
			return err
		}
	}
	_, err := s.qdrant(ctx, http.MethodPut, "/collections/"+name+"/index?wait=true", map[string]any{
		"field_name": "owner_id", "field_schema": "keyword",
	}, nil)
	if err != nil && !strings.Contains(err.Error(), "409") {
		return err
	}
	_, err = s.qdrant(ctx, http.MethodPut, "/collections/"+name+"/index?wait=true", map[string]any{
		"field_name": "created_at_ms", "field_schema": "integer",
	}, nil)
	if err != nil && !strings.Contains(err.Error(), "409") {
		return err
	}
	return nil
}
