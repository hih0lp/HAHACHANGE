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
	service  *rag.Service
	url      string
	model    string
	client   *http.Client
	mcpURL   string
	mcpKey   string
	jiraMode string
}

func (a answerer) Ask(ctx context.Context, question string) (string, []rag.SearchHit, error) {
	hits, err := a.service.Search(ctx, question, 5)
	if err != nil {
		return "", nil, err
	}
	var jiraContext string
	if a.jiraMode == "live" {
		var requested bool
		jiraContext, requested, err = mcpJiraContext(ctx, a.mcpURL, a.mcpKey, question)
		if err != nil && requested {
			return "", hits, fmt.Errorf("Jira MCP: %w", err)
		}
	}
	var sources strings.Builder
	for _, hit := range hits {
		link := hit.SourceURL
		if link == "" {
			link = "https://docs.avito.local/" + hit.Path
		}
		fmt.Fprintf(&sources, "[Источник: %s, URL: %s, фрагмент %d]\n%s\n\n", hit.Path, link, hit.ChunkIndex, hit.Text)
	}
	prompt := "Ответь на вопрос по-русски, используя приведённые фрагменты базы знаний и, если есть, свежие данные Jira MCP. Для фактов из Jira используй только блок Jira MCP и его URL; для фактов из базы знаний укажи ссылку на источник. Если ответа нет, прямо скажи об этом. Не придумывай внутренние инструкции Авито, порядок оформления заявок или API. Не добавляй отдельный список источников в конце ответа.\n\nБаза знаний:\n" + sources.String() + "\nJira MCP:\n" + jiraContext + "\nВопрос: " + question
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
	answerText := result.Choices[0].Message.Content
	if jiraContext != "" {
		var issue jiraIssue
		if json.Unmarshal([]byte(jiraContext), &issue) == nil && issue.URL != "" && !strings.Contains(answerText, issue.URL) {
			answerText += " (Источник Jira: [" + issue.Key + "](" + issue.URL + "))"
		}
		var listed jiraIssues
		if json.Unmarshal([]byte(jiraContext), &listed) == nil {
			for _, item := range listed.Issues {
				if item.URL != "" && !strings.Contains(answerText, item.URL) {
					answerText += " [" + item.Key + "](" + item.URL + ")"
				}
			}
		}
	}
	return answerText, hits, nil
}
