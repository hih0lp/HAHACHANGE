package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (a answerer) groupMembershipsAnswer(ctx context.Context, token string) string {
	const unavailable = "Не удалось проверить список ваших групп в учебном мессенджере."
	if a.groupChatMCPURL == "" {
		return unavailable
	}
	result, err := mcpTeamTool(ctx, a.groupChatMCPURL, "", token, "list_my_groups", map[string]any{})
	if err != nil {
		return unavailable
	}
	raw, _ := json.Marshal(result)
	var data struct {
		Groups []struct {
			Name string `json:"name"`
		} `json:"groups"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Groups == nil {
		return unavailable
	}
	if len(data.Groups) == 0 {
		return "По данным учебного мессенджера вы пока не состоите ни в одной группе."
	}
	lines := []string{"Ваши группы в учебном мессенджере:"}
	for _, group := range data.Groups {
		lines = append(lines, "- "+group.Name)
	}
	return strings.Join(lines, "\n")
}

func (a answerer) groupTopicAnswer(ctx context.Context, token string, plan calendarPlan, now time.Time) string {
	if a.groupChatMCPURL == "" {
		return "Учебный мессенджер сейчас недоступен."
	}
	if plan.ChatGroup == "" && !plan.AllChatGroups {
		return "Укажите название группы, в которой искать сообщения."
	}
	query := strings.TrimSpace(plan.ChatQuery)
	if query == "" || len([]rune(query)) > 300 {
		return "Не удалось определить тему для поиска в учебном мессенджере."
	}
	from, to := plan.ChatFrom, plan.ChatTo
	if from == "" && to == "" {
		from, to = now.Add(-30*24*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339)
	}
	first, err1 := time.Parse(time.RFC3339, from)
	last, err2 := time.Parse(time.RFC3339, to)
	if err1 != nil || err2 != nil || !last.After(first) || last.Sub(first) > 31*24*time.Hour {
		return "Укажите период поиска в мессенджере до 31 дня."
	}
	result, err := mcpTeamTool(ctx, a.groupChatMCPURL, "", token, "search_group_messages", map[string]any{
		"query": query, "group_id": plan.ChatGroup, "from": from, "to": to, "limit": 12,
	})
	if err != nil {
		return "Не удалось выполнить поиск в учебном мессенджере."
	}
	period := first.In(now.Location()).Format("02.01.2006 15:04") + " — " + last.In(now.Location()).Format("02.01.2006 15:04") + " МСК"
	return formatMessengerMessages(result, now.Location(), "В учебном мессенджере по теме «"+query+"» за "+period+":",
		"Мессенджер вернул неполный результат поиска.",
		"В учебном мессенджере не найдено подходящих сообщений за период "+period+".")
}
