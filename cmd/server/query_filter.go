package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type cleanedQuery struct {
	CleanedQuestion string `json:"cleaned_question"`
}

var routeCategories = []string{"jira", "calendar", "gitlab", "documents", "rights", "team"}

const routeInstruction = `Ты языковой редактор. Верни только JSON вида {"cleaned_question":"..."}. Сделай формулировку грамотнее: исправь пунктуацию, убери разговорные частицы и замени очевидные разговорные формы нейтральными. Смысловые слова сохраняй, если не уверен в точной замене. Временные выражения, отрицания, названия продуктов, имена, идентификаторы, числа и каждую часть вопроса копируй посимвольно и в том же порядке. Не переводи и не объясняй. Если сомневаешься, оставь слово без изменений. Не отвечай на вопрос и не определяй инструменты.`

func validateCleanedQuery(route *cleanedQuery) error {
	route.CleanedQuestion = strings.TrimSpace(route.CleanedQuestion)
	if route.CleanedQuestion == "" || len([]rune(route.CleanedQuestion)) > 2000 {
		return errors.New("invalid cleaned question")
	}
	return nil
}

func validateCategories(categories []string) error {
	allowed := map[string]bool{}
	for _, category := range routeCategories {
		allowed[category] = true
	}
	seen := map[string]bool{}
	for _, category := range categories {
		if !allowed[category] || seen[category] {
			return errors.New("invalid route category")
		}
		seen[category] = true
	}
	return nil
}

func (a answerer) filterQuestion(ctx context.Context, question string) (cleanedQuery, error) {
	model := a.routerModel
	if model == "" {
		model = "qwen3-1.7b"
	}
	payload, err := json.Marshal(map[string]any{"model": model, "stream": false, "temperature": 0, "messages": []map[string]string{{"role": "system", "content": routeInstruction}, {"role": "user", "content": question}}})
	if err != nil {
		return cleanedQuery{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.url, "/")+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return cleanedQuery{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := os.Getenv("LLM_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := a.client
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return cleanedQuery{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return cleanedQuery{}, errors.New("query filter unavailable")
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&completion) != nil || len(completion.Choices) == 0 {
		return cleanedQuery{}, errors.New("invalid query filter response")
	}
	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	content = strings.TrimPrefix(strings.TrimPrefix(content, "```json"), "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	var route cleanedQuery
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(content)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&route) != nil || decoder.Decode(new(any)) != io.EOF {
		return cleanedQuery{}, errors.New("invalid query filter JSON")
	}
	if err := validateCleanedQuery(&route); err != nil {
		return cleanedQuery{}, err
	}
	return route, nil
}
