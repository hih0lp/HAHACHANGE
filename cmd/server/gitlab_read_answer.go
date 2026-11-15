package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hackchange/avito-rag/internal/modeloutput"
)

type gitlabProject struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Path string `json:"path_with_namespace"`
}

type gitlabCommit struct {
	ID      string `json:"id"`
	Author  string `json:"author_name"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Created string `json:"created_at"`
}

type gitlabMR struct {
	IID   int    `json:"iid"`
	Title string `json:"title"`
}

func gitlabData[T any](ctx context.Context, endpoint, token, tool string, args map[string]any) (T, error) {
	var out T
	result, err := mcpTeamTool(ctx, endpoint, "", token, tool, args)
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return out, err
	}
	var page struct {
		Data json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(raw, &page); err != nil {
		return out, err
	}
	if len(page.Data) == 0 || string(page.Data) == "null" {
		return out, fmt.Errorf("GitLab returned no data field")
	}
	err = json.Unmarshal(page.Data, &out)
	return out, err
}

func (a answerer) gitLabReadAnswer(ctx context.Context, token string, plan calendarPlan) string {
	projects, err := gitlabData[[]gitlabProject](ctx, a.gitLabMCPURL, token, "gitlab_projects", map[string]any{})
	if err != nil {
		return "Не удалось получить доступные проекты из GitLab."
	}
	if len(projects) == 0 {
		return "GitLab не вернул проектов, доступных вашей учётной записи."
	}
	project, found := chooseGitLabProject(projects, plan.GitLabProject)
	if !found {
		paths := make([]string, 0, len(projects))
		for _, p := range projects {
			paths = append(paths, p.Path)
		}
		return "Укажите проект из доступных в GitLab: " + strings.Join(paths, ", ") + "."
	}
	projectArg := project.Path
	if project.ID > 0 {
		projectArg = strconv.Itoa(project.ID)
	}
	if projectArg == "" {
		return "Не удалось определить идентификатор проекта в GitLab."
	}
	args := map[string]any{"project": projectArg}
	switch plan.GitLabOperation {
	case "commits":
		if plan.GitLabRef != "" {
			args["ref"] = plan.GitLabRef
		}
		commits, err := gitlabData[[]gitlabCommit](ctx, a.gitLabMCPURL, token, "gitlab_commits", args)
		if err != nil {
			return "Не удалось получить коммиты проекта " + project.Path + " из GitLab."
		}
		if len(commits) == 0 {
			return "GitLab не вернул коммитов проекта " + project.Path + " для запрошенной ветки или ссылки."
		}
		sort.SliceStable(commits, func(i, j int) bool { return commits[i].Created > commits[j].Created })
		lines := []string{"Последние коммиты в проекте " + project.Path + ":"}
		for _, c := range commits[:min(len(commits), 5)] {
			lines = append(lines, fmt.Sprintf("- %s — %s. %s, %s.", c.ID, c.Title, c.Author, c.Created))
		}
		return strings.Join(lines, "\n")
	case "commit_diff":
		if plan.GitLabCommit == "" {
			commits, err := gitlabData[[]gitlabCommit](ctx, a.gitLabMCPURL, token, "gitlab_commits", args)
			if err != nil {
				return "Не удалось проверить, какой коммит имелся в виду в GitLab."
			}
			if len(commits) != 1 {
				return "Укажите SHA коммита GitLab, чтобы я мог прочитать его изменения."
			}
			plan.GitLabCommit = commits[0].ID
		}
		args["id"] = plan.GitLabCommit
		return a.gitLabCommitDiff(ctx, token, args, project.Path, plan.GitLabCommit)
	case "merge_requests", "approval":
		return a.gitLabMRAnswer(ctx, token, args, project.Path, plan)
	}
	return "Не удалось определить, что проверить в GitLab."
}

func chooseGitLabProject(projects []gitlabProject, hint string) (gitlabProject, bool) {
	hint = strings.TrimSpace(hint)
	if hint == "" && len(projects) == 1 {
		return projects[0], true
	}
	for _, p := range projects {
		if hint != "" && (strings.EqualFold(hint, p.Path) || strings.EqualFold(hint, p.Name) || hint == strconv.Itoa(p.ID)) {
			return p, true
		}
	}
	return gitlabProject{}, false
}

func (a answerer) gitLabCommitDiff(ctx context.Context, token string, args map[string]any, project, id string) string {
	type fileDiff struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
		Diff    string `json:"diff"`
	}
	diff, err := gitlabData[[]fileDiff](ctx, a.gitLabMCPURL, token, "gitlab_commit_diff", args)
	if err != nil {
		return "Не удалось получить изменения коммита " + id + " из GitLab."
	}
	if len(diff) == 0 {
		return "GitLab не вернул изменений для коммита " + id + "."
	}
	lines := []string{"Изменения коммита " + id + " в " + project + ":"}
	for _, file := range diff[:min(len(diff), 8)] {
		name := file.NewPath
		if file.OldPath != "" && name != file.OldPath {
			name = file.OldPath + " → " + name
		}
		lines = append(lines, "- "+name)
		for _, line := range strings.Split(file.Diff, "\n") {
			if (strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")) ||
				(strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")) {
				lines = append(lines, "  "+modeloutput.PublicTextLimit(line, 240))
				if len(lines) >= 16 {
					break
				}
			}
		}
		if len(lines) >= 16 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func (a answerer) gitLabMRAnswer(ctx context.Context, token string, args map[string]any, project string, plan calendarPlan) string {
	id := strings.TrimPrefix(plan.GitLabMR, "!")
	ordinal := strings.Contains(strings.ToLower(id), "перв") || strings.EqualFold(id, "first")
	if n, err := strconv.Atoi(id); err != nil || n < 1 {
		id = ""
	}
	if plan.GitLabOperation == "merge_requests" || id == "" {
		mrs, err := gitlabData[[]gitlabMR](ctx, a.gitLabMCPURL, token, "gitlab_merge_requests", args)
		if err != nil {
			return "Не удалось проверить открытые merge requests в GitLab."
		}
		if len(mrs) == 0 {
			return "GitLab не вернул открытых merge requests в " + project + "."
		}
		lines := []string{"Открытые merge requests в " + project + ":"}
		for _, mr := range mrs {
			lines = append(lines, fmt.Sprintf("- !%d — %s", mr.IID, mr.Title))
		}
		if plan.GitLabOperation == "merge_requests" {
			return strings.Join(lines, "\n")
		}
		if len(mrs) != 1 && !ordinal {
			return strings.Join(lines, "\n") + "\nУкажите номер merge request для проверки апрува."
		}
		id = strconv.Itoa(mrs[0].IID)
	}
	args["id"] = id
	type rule struct {
		Name     string `json:"name"`
		Required int    `json:"approvals_required"`
		Approved *bool  `json:"approved"`
	}
	state, err := gitlabData[struct {
		Rules []rule `json:"rules"`
	}](ctx, a.gitLabMCPURL, token, "gitlab_approval_state", args)
	if err != nil || len(state.Rules) == 0 {
		return "Не удалось подтвердить статус апрува MR !" + id + " в GitLab."
	}
	lines := []string{"Апрув MR !" + id + " в " + project + ":"}
	for _, r := range state.Rules {
		if r.Approved == nil {
			continue
		}
		status := "апрув пока не получен"
		if *r.Approved {
			status = "одобрено"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (требуется апрувов: %d).", r.Name, status, r.Required))
	}
	if len(lines) == 1 {
		return "GitLab не вернул подтверждённый статус апрува MR !" + id + "."
	}
	return strings.Join(lines, "\n")
}
