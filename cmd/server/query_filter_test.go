package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryFilterUsesSmallModelAndMultipleCategories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		if in.Model != "qwen3-1.7b" || !strings.Contains(in.Messages[0].Content, "ВСЕ категории") || !strings.Contains(in.Messages[1].Content, "джире") {
			t.Fatalf("request=%+v", in)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"cleaned_question\":\"Что завтра в онбординге и какие задачи в Jira?\",\"categories\":[\"calendar\",\"jira\"]}"}}]}`)
	}))
	defer server.Close()
	route, err := (answerer{url: server.URL, client: server.Client()}).filterQuestion(context.Background(), "А че завтра по онбордингу и задачки в джире?")
	if err != nil || strings.Join(route.Categories, ",") != "calendar,jira" || !strings.Contains(route.CleanedQuestion, "Jira") {
		t.Fatalf("route=%+v err=%v", route, err)
	}
}

func TestQueryFilterRejectsInvalidResponses(t *testing.T) {
	for _, route := range []queryRoute{{Categories: []string{"jira"}}, {CleanedQuestion: "вопрос", Categories: []string{"unknown"}}, {CleanedQuestion: "вопрос", Categories: []string{"team", "team"}}} {
		if err := validateQueryRoute(&route); err == nil {
			t.Fatalf("accepted %+v", route)
		}
	}
	for _, response := range []string{`not-json`, `{"cleaned_question":"","categories":[]}`, `{"cleaned_question":"ok","categories":["unknown"]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, response)
		}))
		_, err := (answerer{url: server.URL, client: server.Client()}).filterQuestion(context.Background(), "вопрос")
		server.Close()
		if err == nil {
			t.Fatalf("accepted %s", response)
		}
	}
}

func TestPlannerCategoriesAreValidated(t *testing.T) {
	if err := validateCategories([]string{"documents", "team"}); err != nil {
		t.Fatal(err)
	}
	if err := validateCategories([]string{}); err != nil {
		t.Fatal(err)
	}
	if err := validateCategories([]string{"gitlab", "gitlab"}); err == nil {
		t.Fatal("duplicate category accepted")
	}
	if err := validateCategories([]string{"unknown"}); err == nil {
		t.Fatal("unknown category accepted")
	}
}

func TestGitLabProjectIDsFromLiveCatalogue(t *testing.T) {
	ids := gitlabProjectIDs(map[string]any{"data": []any{map[string]any{"id": float64(2)}, map[string]any{"id": float64(17)}}})
	if strings.Join(ids, ",") != "2,17" {
		t.Fatalf("ids=%v", ids)
	}
	if ids := gitlabProjectIDs(map[string]any{"data": "invalid"}); len(ids) != 0 {
		t.Fatalf("invalid ids=%v", ids)
	}
}

func TestPersonalGitLabEvidenceUsesVerifiedID(t *testing.T) {
	identity := gitlabIdentityID(map[string]any{"id": float64(42), "username": "intern"})
	if identity != "42" {
		t.Fatalf("identity=%q", identity)
	}
	if got := gitlabIdentityID(map[string]any{"username": "intern"}); got != "" {
		t.Fatalf("missing identity=%q", got)
	}
	if got := gitlabIdentityID(make(chan int)); got != "" {
		t.Fatalf("invalid identity=%q", got)
	}
	if got := gitlabIdentityID(map[string]any{"id": "not-a-number"}); got != "" {
		t.Fatalf("malformed identity=%q", got)
	}
	mrs := map[string]any{"data": []any{
		map[string]any{"iid": float64(1), "author": map[string]any{"id": float64(42)}},
		map[string]any{"iid": float64(2), "author": map[string]any{"id": float64(7)}, "reviewers": []any{map[string]any{"id": float64(42)}}},
		map[string]any{"iid": float64(3), "author": map[string]any{"id": float64(7)}, "reviewers": []any{map[string]any{"id": float64(8)}}},
	}}
	selected := personalGitLabMRs(mrs, identity)
	if len(selected) != 2 || selected[0]["iid"] != float64(1) || selected[1]["iid"] != float64(2) {
		t.Fatalf("selected=%v", selected)
	}
	if result := personalGitLabMRs(mrs, ""); len(result) != 0 {
		t.Fatalf("unverified identity: %v", result)
	}
}

func TestCompactGitLabMREvidence(t *testing.T) {
	items := []map[string]any{{"iid": float64(3), "title": "Add checks", "web_url": "https://gitlab.example/mr/3", "description": strings.Repeat("a", 700), "private_note": "ignore"}}
	compact := compactGitLabMRs(items)
	if len(compact) != 1 || compact[0]["private_note"] != nil || len([]rune(compact[0]["description"].(string))) != 600 {
		t.Fatalf("compact=%v", compact)
	}
}

func TestRecommendationRequiresVerifiedPersonalAndDocumentEvidence(t *testing.T) {
	sources := map[string]string{"jira:SCRUM-7": "[SCRUM-7](https://jira.example/7)", "doc:0": "[Guide](https://docs.example/guide)"}
	answer, err := renderRecommendationJSON(`{"steps":[{"action":"Изучить проверку входных данных", "sources":["jira:SCRUM-7","doc:0"]}]}`, sources, true)
	if err != nil || !strings.Contains(answer, "SCRUM-7") || !strings.Contains(answer, "не оценка прежнего опыта") {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	for _, response := range []string{
		`{"steps":[{"action":"Изучить SCRUM-6", "sources":["jira:SCRUM-7","doc:0"]}]}`,
		`{"steps":[{"action":"Изучить проверку", "sources":["jira:SCRUM-6","doc:0"]}]}`,
		`{"steps":[{"action":"", "sources":["jira:SCRUM-7","doc:0"]}]}`,
	} {
		if _, err := renderRecommendationJSON(response, sources, false); err == nil {
			t.Fatalf("accepted %s", response)
		}
	}
	if answer, err := renderRecommendationJSON(`{"steps":[{"action":"Изучить проверку", "sources":["doc:0"]}]}`, sources, false); err != nil || !strings.Contains(answer, "Не удалось связать") {
		t.Fatalf("ungrounded=%q err=%v", answer, err)
	}
	if answer, err := renderRecommendationJSON(`{"steps":[]}`, sources, false); err != nil || !strings.Contains(answer, "Не удалось связать") {
		t.Fatalf("empty=%q err=%v", answer, err)
	}
}

func TestRecommendationUsesDetailedAssignedIssueOnly(t *testing.T) {
	issue := jiraIssue{Key: "SCRUM-7", AssigneeAccountID: "me", URL: "https://jira.example/browse/SCRUM-7"}
	if recommendationIssueEligible(issue, "SCRUM-7", "me") {
		t.Fatal("empty description accepted")
	}
	issue.Description = "Implement request validation"
	if !recommendationIssueEligible(issue, "SCRUM-7", "me") {
		t.Fatal("described assigned issue rejected")
	}
	if recommendationIssueEligible(issue, "SCRUM-7", "other") {
		t.Fatal("foreign assignee accepted")
	}
	if recommendationIssueEligible(issue, "SCRUM-8", "me") {
		t.Fatal("wrong key accepted")
	}
}

func TestPlaceholderURLsAreRemovedFromEvidence(t *testing.T) {
	input := map[string]any{"profile": map[string]any{"url": "https://example.invalid/user", "name": "Intern"}, "links": []any{"https://jira.example.com/browse/SCRUM-7"}}
	clean := removePlaceholderURLs(input).(map[string]any)
	profile := clean["profile"].(map[string]any)
	if profile["url"] != "" || profile["name"] != "Intern" || clean["links"].([]any)[0] != "https://jira.example.com/browse/SCRUM-7" {
		t.Fatalf("clean=%v", clean)
	}
}

func TestPersonalJiraListUsesOnlyAssignedFields(t *testing.T) {
	list := map[string]any{"project_key": "SCRUM", "issues": []any{map[string]any{"key": "SCRUM-7", "summary": "Название", "status": "В работе", "due_date": "2026-11-13", "assignee_account_id": "me", "url": "https://jira.example/browse/SCRUM-7"}}}
	answer, err := renderPersonalJiraList(list, "me")
	if err != nil || !strings.Contains(answer, "SCRUM-7") || !strings.Contains(answer, "2026-11-13") || strings.Contains(strings.ToLower(answer), "описание") {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if _, err := renderPersonalJiraList(list, "other"); err == nil {
		t.Fatal("foreign issue accepted")
	}
	if answer, err := renderPersonalJiraList(map[string]any{"issues": []any{}}, "me"); err != nil || !strings.Contains(answer, "нет") {
		t.Fatalf("empty answer=%q err=%v", answer, err)
	}
	if _, err := renderPersonalJiraList(map[string]any{"issues": "bad"}, "me"); err == nil {
		t.Fatal("invalid issue payload accepted")
	}
	list["truncated"] = true
	if answer, err := renderPersonalJiraList(list, "me"); err != nil || !strings.Contains(answer, "первая страница") {
		t.Fatalf("truncated answer=%q err=%v", answer, err)
	}
}

func TestOpenGitLabListContainsOnlyVerifiedMRFields(t *testing.T) {
	projects := map[string]any{"2": map[string]any{"data": []any{map[string]any{"iid": float64(7), "title": "Add checks", "web_url": "https://gitlab.example/mr/7", "author": map[string]any{"name": "Other"}}}, "next_page": ""}}
	answer, err := renderOpenGitLabMRs(projects, false)
	if err != nil || !strings.Contains(answer, "!7") || !strings.Contains(answer, "Add checks") || strings.Contains(answer, "Other") {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if _, err := renderOpenGitLabMRs(map[string]any{"2": map[string]any{"data": []any{map[string]any{"iid": 7}}}}, false); err == nil {
		t.Fatal("incomplete MR accepted")
	}
	answer, err = renderOpenGitLabMRs(map[string]any{}, true)
	if err != nil || !strings.Contains(answer, "нет") || !strings.Contains(answer, "часть") {
		t.Fatalf("empty answer=%q err=%v", answer, err)
	}
}
