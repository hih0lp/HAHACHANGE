package main

import (
	"context"
	"encoding/json"
	"strings"
)

func (a answerer) gitLabProjectsAnswer(ctx context.Context, token string) string {
	if token == "" {
		return "Войдите в Боббика, чтобы проверить доступные вам проекты GitLab."
	}
	const unavailable = "Не удалось получить список проектов GitLab."
	result, err := mcpTeamTool(ctx, a.gitLabMCPURL, "", token, "gitlab_projects", map[string]any{})
	if err != nil {
		return unavailable
	}
	data, err := json.Marshal(result)
	if err != nil {
		return unavailable
	}
	var page struct {
		Projects []struct {
			Name string `json:"name"`
			Path string `json:"path_with_namespace"`
		} `json:"data"`
		NextPage string `json:"next_page"`
	}
	if err := json.Unmarshal(data, &page); err != nil || page.Projects == nil {
		return unavailable
	}
	if len(page.Projects) == 0 {
		return "GitLab не вернул проектов, доступных вашей учётной записи."
	}
	lines := []string{"Проекты GitLab, доступные вашей учётной записи:"}
	for _, project := range page.Projects {
		label := project.Name
		if project.Path != "" && project.Path != project.Name {
			label += " (" + project.Path + ")"
		}
		lines = append(lines, "- "+label)
	}
	if page.NextPage != "" {
		lines = append(lines, "Показана первая страница проектов.")
	}
	return strings.Join(lines, "\n")
}
