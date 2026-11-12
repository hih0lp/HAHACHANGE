package main

import (
	_ "embed"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/hackchange/avito-rag/internal/rag"
)

//go:embed mcp-guide.md
var mcpUsageGuide string

type answerEvidence struct {
	required, failed, verified, unavailableCatalogues map[string]bool
}

func newAnswerEvidence() answerEvidence {
	return answerEvidence{
		required: make(map[string]bool), failed: make(map[string]bool),
		verified: make(map[string]bool), unavailableCatalogues: make(map[string]bool),
	}
}

func toolSource(name string) string {
	source, _, _ := strings.Cut(name, "_")
	return source
}

func (e answerEvidence) require(sources ...string) {
	for _, source := range sources {
		switch source {
		case "gitlab", "jira", "calendar", "team", "rights":
			e.required[source] = true
		}
	}
}

func (e answerEvidence) catalogueError(source string, err error) {
	if err != nil {
		e.unavailableCatalogues[source] = true
		log.Printf("MCP tools/list unavailable: source=%s", source)
	}
}

func (e answerEvidence) record(source string, err error) {
	if err != nil {
		e.failed[source] = true
		log.Printf("MCP tools/call failed: source=%s", source)
		return
	}
	e.verified[source] = true
	delete(e.failed, source)
}

func (e answerEvidence) unavailable() []string {
	var sources []string
	for source := range e.required {
		if e.unavailableCatalogues[source] {
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)
	return sources
}

func (e answerEvidence) missing() []string {
	missing := make(map[string]bool)
	for source := range e.required {
		if !e.verified[source] {
			missing[source] = true
		}
	}
	for source := range e.failed {
		missing[source] = true
	}
	var sources []string
	for source := range missing {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func (e answerEvidence) fallback(sources []string, hits []rag.SearchHit) string {
	names := map[string]string{"gitlab": "GitLab", "jira": "Jira", "calendar": "Google Calendar", "team": "Профиль команды", "rights": "Сервис доступов"}
	links := map[string]string{
		"gitlab":   env("GITLAB_WEB_URL", os.Getenv("GITLAB_API_ORIGIN")),
		"jira":     os.Getenv("JIRA_BASE_URL"),
		"calendar": env("CALENDAR_WEB_URL", "https://calendar.google.com/calendar/"),
		"team":     "/onboarding", "rights": "/onboarding",
	}
	var items []string
	for _, source := range sources {
		name, link := names[source], links[source]
		if manualLink(link) || link == "/onboarding" {
			name = "[" + name + "](" + link + ")"
		}
		items = append(items, name)
	}
	answer := "Не удалось подтвердить запрошенные данные через подключённые инструменты. Проверьте источник вручную: " + strings.Join(items, ", ") + "."
	var materials []string
	seen := make(map[string]bool)
	for _, hit := range hits {
		if manualLink(hit.SourceURL) && !seen[hit.SourceURL] {
			materials = append(materials, "- ["+hit.SourceURL+"]("+hit.SourceURL+")")
			seen[hit.SourceURL] = true
		}
		if len(materials) == 3 {
			break
		}
	}
	if len(materials) > 0 {
		answer += "\n\nНиже только материалы из поиска по документам. Они не подтверждают актуальные данные сервиса.\n" + strings.Join(materials, "\n")
	}
	return answer
}

func manualLink(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") && !strings.ContainsAny(raw, " \r\n\t()<>")
}
