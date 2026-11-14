package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (a answerer) latestGroupMessages(ctx context.Context, token, group string, loc *time.Location) string {
	const unavailable = "Не удалось получить последние сообщения мессенджера."
	result, err := mcpTeamTool(ctx, a.groupChatMCPURL, "", token, "latest_group_messages", map[string]any{"group": group})
	if err != nil {
		return unavailable
	}
	data, err := json.Marshal(result)
	if err != nil {
		return unavailable
	}
	var history struct {
		Messages []struct {
			Group  string    `json:"group_name"`
			Author string    `json:"author_name"`
			Time   time.Time `json:"sent_at"`
			Text   string    `json:"text"`
		} `json:"messages"`
	}
	if json.Unmarshal(data, &history) != nil || history.Messages == nil {
		return unavailable
	}
	if len(history.Messages) == 0 {
		return "Мессенджер не вернул доступных сообщений для этого запроса."
	}
	lines := []string{"Последние сообщения в мессенджере"}
	for _, m := range history.Messages {
		lines = append(lines, "- "+m.Time.In(loc).Format("02.01 15:04")+" МСК · "+m.Group+" · "+m.Author+" — "+m.Text)
	}
	return strings.Join(lines, "\n")
}
