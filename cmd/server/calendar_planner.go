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

type calendarPlan struct {
	NeedsEvents         bool   `json:"needs_events"`
	NeedsAvailability   bool   `json:"needs_availability"`
	NeedsForm           bool   `json:"needs_form"`
	NeedsMyJiraIssues   bool   `json:"needs_my_jira_issues"`
	NeedsRecommendation bool   `json:"needs_recommendation"`
	NeedsTeamDirectory  bool   `json:"needs_team_directory"`
	FormID              string `json:"form_id"`
	PeriodKind          string `json:"period_kind"`
	Historical          bool   `json:"historical"`
	TimeMin             string `json:"time_min"`
	TimeMax             string `json:"time_max"`
}

type formChoice struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	URL         string `json:"form_url"`
}

// The model chooses whether calendar evidence is needed and resolves the
// requested interval. The server validates its plan before calling MCP.
func (a answerer) planCalendar(ctx context.Context, question string, now time.Time, forms []formChoice) (calendarPlan, error) {
	instruction := "Classify the user's requested evidence and action. Return only JSON with booleans needs_events, needs_availability, needs_form, needs_my_jira_issues, needs_recommendation, needs_team_directory, historical; form_id; period_kind of current_week, next_week, explicit, or upcoming; and RFC3339 time_min and time_max. Set needs_team_directory when the answer requires verified current membership, people, roles, mentor, or lead in the user's team; do not set it for a generic question about teams. Set needs_my_jira_issues when the user asks about tasks assigned to themselves; a project-wide list is not evidence of personal assignment. Set needs_recommendation when the user asks for personalized learning or development advice. If the user asks how or where to submit an application, or asks to obtain a resource through a request, set needs_form true and choose the matching form_id from the supplied live catalogue. Choose by the form's purpose, not by exact words. If intent is ambiguous, leave form_id empty so the server can show available forms. Never invent a form ID. For a question asking when the mentor is busy, available or free, set needs_availability true and choose the requested time range. Set needs_events when the question asks which scheduled events occur or for their details; both flags may be true if both kinds of evidence are needed. For calendar questions, use current Europe/Moscow time. Choose current_week for the ongoing calendar week and next_week for the following week; the server computes their Monday boundaries. Set historical true only for explicitly past events. For an unspecified future schedule use the next 30 days. If no calendar evidence is needed, leave times empty. Do not answer the user's question."
	formData, _ := json.Marshal(forms)
	request := map[string]any{"model": a.model, "stream": false, "messages": []map[string]string{{"role": "system", "content": instruction}, {"role": "user", "content": "Current time: " + now.Format(time.RFC3339) + "\nAvailable forms: " + string(formData) + "\nQuestion: " + question}}}
	body, err := json.Marshal(request)
	if err != nil {
		return calendarPlan{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.url, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return calendarPlan{}, err
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
		return calendarPlan{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return calendarPlan{}, errors.New("calendar planner unavailable")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result); err != nil || len(result.Choices) == 0 {
		return calendarPlan{}, errors.New("invalid calendar planner response")
	}
	content := strings.TrimSpace(result.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	var plan calendarPlan
	if json.Unmarshal([]byte(strings.TrimSpace(content)), &plan) != nil {
		return calendarPlan{}, errors.New("invalid calendar plan")
	}
	return normalizeCalendarPlan(plan, now)
}

func normalizeCalendarPlan(plan calendarPlan, now time.Time) (calendarPlan, error) {
	if !plan.NeedsEvents && !plan.NeedsAvailability {
		return plan, nil
	}
	if plan.PeriodKind == "current_week" || plan.PeriodKind == "next_week" {
		local := now.In(now.Location())
		weekday := (int(local.Weekday()) + 6) % 7
		monday := time.Date(local.Year(), local.Month(), local.Day()-weekday, 0, 0, 0, 0, local.Location())
		if plan.PeriodKind == "next_week" {
			monday = monday.AddDate(0, 0, 7)
		}
		plan.TimeMin = monday.Format(time.RFC3339)
		plan.TimeMax = monday.AddDate(0, 0, 7).Format(time.RFC3339)
	}
	start, err := time.Parse(time.RFC3339, plan.TimeMin)
	if err != nil {
		return calendarPlan{}, errors.New("calendar start is invalid")
	}
	end, err := time.Parse(time.RFC3339, plan.TimeMax)
	if err != nil || !end.After(start) || end.Sub(start) > 31*24*time.Hour {
		return calendarPlan{}, errors.New("calendar range is invalid")
	}
	if !plan.Historical && start.Before(now) {
		start = now
		plan.TimeMin = start.Format(time.RFC3339)
	}
	if !end.After(start) {
		return calendarPlan{}, errors.New("calendar range has elapsed")
	}
	return plan, nil
}
