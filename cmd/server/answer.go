package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hackchange/avito-rag/internal/rag"
)

type answerer struct {
	service *rag.Service
	url     string
	model   string
	client  *http.Client
}

func (a answerer) Ask(ctx context.Context, question string) (string, []rag.SearchHit, error) {
	hits, err := a.service.Search(ctx, question, 5)
	if err != nil {
		return "", nil, err
	}
	var sources strings.Builder
	for _, hit := range hits {
		link := hit.SourceURL
		if link == "" {
			link = "https://docs.avito.local/" + hit.Path
		}
		fmt.Fprintf(&sources, "[Источник: %s, URL: %s, фрагмент %d]\n%s\n\n", hit.Path, link, hit.ChunkIndex, hit.Text)
	}
	prompt := "Ответь на вопрос по-русски, используя только приведённые фрагменты базы знаний (открытый Playbook и чек-лист стажёра). Если ответа в них нет, прямо скажи об этом. Не придумывай внутренние инструкции Авито, порядок оформления заявок или API. Отделяй сведения из источников от предположений. После каждого утверждения укажи источник прямо в том же абзаце как Markdown-ссылку [имя файла](URL). Используй только URL, указанные у фрагментов; не придумывай другие адреса. Не добавляй отдельный список или раздел источников в конце ответа.\n\n" + sources.String() + "\nВопрос: " + question
	body, err := json.Marshal(map[string]any{"model": a.model, "stream": false, "messages": []map[string]string{{"role": "user", "content": prompt}}})
	if err != nil {
		return "", hits, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.url, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", hits, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := a.client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", hits, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", hits, err
	}
	if resp.StatusCode/100 != 2 {
		return "", hits, fmt.Errorf("LLM returned HTTP %d: %s", resp.StatusCode, responseBody)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", hits, err
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", hits, fmt.Errorf("LLM returned an empty answer")
	}
	return result.Choices[0].Message.Content, hits, nil
}
