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

type queryRoute struct {
	CleanedQuestion string   `json:"cleaned_question"`
	Categories      []string `json:"categories"`
}

var routeCategories = []string{"jira", "calendar", "gitlab", "documents", "rights", "team"}

const routeInstruction = `Ты фильтр запросов. Верни только JSON с cleaned_question и categories. Допустимые категории: jira — трекер задач и статусы задач; calendar — встречи, даты и слоты наставника; gitlab — код, проекты, коммиты и merge requests; documents — документы и инструкции; rights — права, доступ и заявки; team — сотрудники, роли, наставники и профиль. Выбери ВСЕ категории, без которых нельзя ответить на каждую часть запроса. Для личных задач добавь team для проверки личности. Для персональных рекомендаций добавь jira, gitlab, documents и team. Не заменяй упомянутый сервис другим: Jira и GitLab — разные системы. Перепиши только разговорные обороты. Сохрани имена систем, отрицания, даты, ключи задач и смысл всех частей. Если запрос смешанный, сохрани каждую часть. Не отвечай на вопрос.`

func validateQueryRoute(route *queryRoute) error {
	route.CleanedQuestion = strings.TrimSpace(route.CleanedQuestion)
	if route.CleanedQuestion == "" || len([]rune(route.CleanedQuestion)) > 2000 {
		return errors.New("invalid cleaned question")
	}
	allowed := map[string]bool{}
	for _, category := range routeCategories {
		allowed[category] = true
	}
	seen := map[string]bool{}
	for _, category := range route.Categories {
		if !allowed[category] || seen[category] {
			return errors.New("invalid route category")
		}
		seen[category] = true
	}
	return nil
}

func (a answerer) filterQuestion(ctx context.Context, question string) (queryRoute, error) {
	model := a.routerModel
	if model == "" {
		model = "qwen3-1.7b"
	}
	payload, err := json.Marshal(map[string]any{"model": model, "stream": false, "temperature": 0, "messages": []map[string]string{{"role": "system", "content": routeInstruction}, {"role": "user", "content": question}}})
	if err != nil {
		return queryRoute{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.url, "/")+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return queryRoute{}, err
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
		return queryRoute{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return queryRoute{}, errors.New("query filter unavailable")
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&completion) != nil || len(completion.Choices) == 0 {
		return queryRoute{}, errors.New("invalid query filter response")
	}
	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	content = strings.TrimPrefix(strings.TrimPrefix(content, "```json"), "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	var route queryRoute
	if json.Unmarshal([]byte(strings.TrimSpace(content)), &route) != nil {
		return queryRoute{}, errors.New("invalid query filter JSON")
	}
	if err := validateQueryRoute(&route); err != nil {
		return queryRoute{}, err
	}
	return route, nil
}
