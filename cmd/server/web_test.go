package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebPageShowsOnlyAnswerWithInlineLinks(t *testing.T) {
	response := httptest.NewRecorder()
	webPage(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	page := response.Body.String()
	for _, expected := range []string{
		"fetch('/v1/jobs'",
		"appendMessage(answer,job.answer)",
		"link.textContent=match[1]",
		"link.rel='noopener noreferrer'",
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("page missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"Только поиск", "sourceCard(", "job.sources", "data.hits", "fetch('/v1/knowledge/search'", "Тестовая заявка на доступ", "access-form", "/v1/access-requests",
	} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page unexpectedly contains %q", forbidden)
		}
	}
	if !strings.Contains(page, "AI-buddy · напоминание") || !strings.Contains(page, "/v1/reminders/ws") {
		t.Fatal("reminder message flow is missing")
	}
}

func TestWebPageIsChatOnly(t *testing.T) {
	response := httptest.NewRecorder()
	webPage(response, httptest.NewRequest(http.MethodGet, "/", nil))
	page := response.Body.String()
	for _, expected := range []string{`aria-label="Сообщения чата"`, `bubble user`, `bubble assistant`, `fetch('/v1/jobs'`} {
		if !strings.Contains(page, expected) {
			t.Errorf("page missing %q", expected)
		}
	}
	for _, forbidden := range []string{`data-scenario=`, `Учебное пространство команды`, `Тестовая заявка на доступ`} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page unexpectedly contains %q", forbidden)
		}
	}
}
