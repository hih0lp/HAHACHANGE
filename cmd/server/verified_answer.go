package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/hackchange/avito-rag/internal/rag"
)

var documentCitation = regexp.MustCompile(`\[D([0-9]+)\]`)

func insertVerifiedDocumentReferences(answer string, hits []rag.SearchHit) string {
	return documentCitation.ReplaceAllStringFunc(answer, func(reference string) string {
		match := documentCitation.FindStringSubmatch(reference)
		index, err := strconv.Atoi(match[1])
		if err != nil || index < 1 || index > len(hits) {
			return "[источник не подтверждён]"
		}
		hit := hits[index-1]
		label := strings.NewReplacer("[", "", "]", "").Replace(hit.Path)
		target, err := url.Parse(hit.SourceURL)
		if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
			return "(" + label + ")"
		}
		href := strings.NewReplacer("(", "%28", ")", "%29").Replace(target.String())
		return "[" + label + "](" + href + ")"
	})
}

func promisesToolCall(answer string) bool {
	text := strings.ToLower(strings.TrimSpace(answer))
	if strings.Contains(text, "вызову") && (strings.Contains(text, "функц") || strings.Contains(text, "инструмент")) {
		return !strings.Contains(text, "\n- ") && !strings.Contains(text, "\n1.")
	}
	return strings.HasPrefix(text, "пожалуйста, подождите")
}

func isPlaybookReadmeFollowup(question string, recent []chatContextTurn) bool {
	q := strings.ToLower(question)
	if !(strings.Contains(q, "ридми") || strings.Contains(q, "readme")) ||
		!(strings.Contains(q, "каком") || strings.Contains(q, "какой") || strings.Contains(q, "какого") || strings.Contains(q, "где")) ||
		strings.Contains(q, "gitlab") || strings.Contains(q, "гитлаб") {
		return false
	}
	for i := len(recent) - 1; i >= 0 && i >= len(recent)-2; i-- {
		context := strings.ToLower(recent[i].Question + " " + recent[i].Answer)
		if strings.Contains(context, "playbook") || strings.Contains(context, "авито") {
			return !strings.Contains(context, "demo/bobby-playbook")
		}
	}
	return strings.Contains(q, "playbook") || strings.Contains(q, "авито")
}

func hasVerifiedDocumentCitation(answer string, hits []rag.SearchHit) bool {
	for _, m := range documentCitation.FindAllStringSubmatch(answer, -1) {
		index, err := strconv.Atoi(m[1])
		if err == nil && index >= 1 && index <= len(hits) && hits[index-1].Path != "" {
			return true
		}
	}
	return false
}

func verifiedDocumentFallback(hits []rag.SearchHit) string {
	lines := []string{"Не удалось подтвердить краткий ответ по найденным фрагментам. Вот материалы, которые вернул поиск:"}
	seen := map[string]bool{}
	for i, h := range hits {
		if h.Path == "" || seen[h.Path] {
			continue
		}
		seen[h.Path] = true
		name := h.Title
		if name == "" {
			name = h.Path
		}
		lines = append(lines, fmt.Sprintf("- %s [D%d]", name, i+1))
		if len(lines) == 4 {
			break
		}
	}
	return strings.Join(lines, "\n")
}
