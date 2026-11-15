package main

import (
	"regexp"
	"strings"
)

var explicitChatGroup = regexp.MustCompile("(?i)групп[еуыеи]\\s+[«\"]([^»\"]+)[»\"]")
var searchTicket = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*-[0-9]+\b`)

// Preserve explicit scope even when the model carries an unrelated intent
// forward from the preceding conversation.
func refineExplicitScope(plan *calendarPlan, question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	changed := false
	oneOnOne := strings.Contains(q, "1:1") || strings.Contains(q, "1 на 1") || strings.Contains(q, "1-на-1")
	if oneOnOne && (strings.Contains(q, "когда") || strings.Contains(q, "доступ") || strings.Contains(q, "окн") || strings.Contains(q, "слот")) {
		plan.NeedsEvents, plan.NeedsOneOnOne, plan.NeedsAvailability = true, true, false
		if !strings.Contains(q, "документ") && !strings.Contains(q, "регламент") {
			plan.NeedsDocumentEvidence, plan.NeedsRecommendation = false, false
		}
		if strings.Contains(q, "на неделе") && !strings.Contains(q, "прошл") {
			plan.PeriodKind, plan.DaysAhead = "upcoming", 7
			plan.TimeMin, plan.TimeMax = "", ""
		}
		changed = true
	}
	if strings.Contains(q, "задач") && (strings.Contains(q, "у меня") || strings.Contains(q, "мои задач")) &&
		(strings.Contains(q, "какие") || strings.Contains(q, "посмотри") || strings.Contains(q, "покажи") || strings.Contains(q, "перечисли")) &&
		!strings.Contains(q, "в чате") && !strings.Contains(q, "показывал") && !strings.Contains(q, "в документа") && !strings.Contains(q, " и ") {
		plan.NeedsMyJiraIssues = true
		plan.NeedsDocumentEvidence, plan.NeedsRecommendation = false, false
		changed = true
	}
	if key := searchTicket.FindString(strings.ToUpper(question)); key != "" &&
		(strings.Contains(q, "в задаче") || strings.Contains(q, "по задаче") || strings.Contains(q, "карточк") || strings.Contains(q, "задачу")) &&
		!explicitChatGroup.MatchString(question) {
		plan.NeedsJiraIssue = true
		if len(plan.JiraIssueKeys) == 0 {
			plan.JiraIssueKeys = []string{key}
		}
		if !strings.Contains(q, " и ") {
			plan.NeedsDocumentEvidence, plan.NeedsRecommendation = false, false
		}
		changed = true
	}
	if match := explicitChatGroup.FindStringSubmatch(question); len(match) == 2 {
		plan.NeedsGroupChat, plan.AllChatGroups = true, false
		if !strings.Contains(q, "документ") {
			plan.NeedsDocumentEvidence, plan.NeedsRecommendation = false, false
		}
		plan.ChatGroup = strings.TrimSpace(match[1])
		if key := searchTicket.FindString(question); key != "" {
			plan.ChatQuery = key
			plan.LatestGroupMessages = false
		}
		changed = true
	}
	return changed
}

func contextualGoDocumentation(question string, recent []chatContextTurn) string {
	q := strings.ToLower(question)
	if !strings.Contains(" "+q+" ", " go ") || !strings.Contains(q, "разработ") || strings.Contains(q, "задач") {
		return ""
	}
	if strings.Contains(q, "авито") || strings.Contains(q, "avito") {
		return "Инженерные практики разработки на Go в открытом AvitoTech Playbook"
	}
	for i := len(recent) - 1; i >= 0 && i >= len(recent)-2; i-- {
		previous := strings.ToLower(recent[i].Question + " " + recent[i].Answer)
		if strings.Contains(previous, "авито") || strings.Contains(previous, "playbook/") {
			return "Инженерные практики разработки на Go в открытом AvitoTech Playbook"
		}
	}
	return ""
}
