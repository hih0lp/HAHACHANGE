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
		"Только поиск", "sourceCard(", "job.sources", "data.hits", "fetch('/v1/knowledge/search'",
	} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page unexpectedly contains %q", forbidden)
		}
	}
}

func TestWebPageOffersThreeScenariosThroughExistingJobFlow(t *testing.T) {
	response := httptest.NewRecorder()
	webPage(response, httptest.NewRequest(http.MethodGet, "/", nil))
	page := response.Body.String()
	for _, expected := range []string{
		`data-scenario="first-week"`,
		`data-scenario="access"`,
		`data-scenario="absence"`,
		`form.requestSubmit()`,
		`fetch('/v1/jobs'`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("page missing %q", expected)
		}
	}
}
