package rag

import (
	"context"
	"net/http"
)

func (s *Service) FindDocumentByPath(ctx context.Context, path string) (SearchHit, bool, error) {
	var response struct {
		Result struct {
			Points []struct {
				Payload payload `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	_, err := s.qdrant(ctx, http.MethodPost, "/collections/"+s.cfg.CollectionName+"/points/scroll", map[string]any{
		"limit": 1, "with_payload": true, "with_vector": false,
		"filter": map[string]any{"must": []any{
			map[string]any{"key": "path", "match": map[string]string{"value": path}},
		}},
	}, &response)
	if err != nil {
		return SearchHit{}, false, err
	}
	if len(response.Result.Points) == 0 || response.Result.Points[0].Payload.Path != path {
		return SearchHit{}, false, nil
	}
	p := response.Result.Points[0].Payload
	return SearchHit{Path: p.Path, Title: p.Title, SourceURL: sourceURL(p.Path), ChunkIndex: p.ChunkIndex, Text: p.Text}, true, nil
}
