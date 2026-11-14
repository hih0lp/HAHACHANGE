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
		"link.textContent=label",
		"link.className='form-link'",
		"link.href=href",
		"event.preventDefault();openInlineForm",
		"const formID=href.slice('/forms/'.length)",
		"link.rel='noopener noreferrer'",
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("page missing %q", expected)
		}
	}
	if strings.Contains(page, "openInlineForm(target.closest('.bubble'),match[2]") {
		t.Fatal("form link handler captures the mutable parser match instead of its form ID")
	}
	for _, forbidden := range []string{
		"Только поиск", "sourceCard(", "job.sources", "data.hits", "fetch('/v1/knowledge/search'", "Тестовая заявка на доступ", "access-form", "/v1/access-requests",
	} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page unexpectedly contains %q", forbidden)
		}
	}
	if !strings.Contains(page, "/v1/deadline-notifications") || !strings.Contains(page, "Скоро дедлайн") {
		t.Fatal("deadline notification flow is missing")
	}
	if strings.Contains(page, "/v1/reminders/ws") {
		t.Fatal("legacy reminder subscription would duplicate deadline cards")
	}
	for _, expected := range []string{"function messageTime(", "job.completed_at||job.created_at", "item.created_at", "timeZone:'Europe/Moscow'"} {
		if !strings.Contains(page, expected) {
			t.Errorf("timestamp UI missing %q", expected)
		}
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
	for _, expected := range []string{`openInlineForm`, `Подтвердить отправку`, `Idempotency-Key`, `teamSessionReady`} {
		if !strings.Contains(page, expected) {
			t.Errorf("chat form missing %q", expected)
		}
	}
	for _, forbidden := range []string{`data-scenario=`, `Учебное пространство команды`, `Тестовая заявка на доступ`} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page unexpectedly contains %q", forbidden)
		}
	}
}

func TestOldFormLinkReturnsToChat(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/forms/access-request", nil)
	request.SetPathValue("id", "access-request")
	formPage(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/?form=access-request" {
		t.Fatalf("status %d, location %q", response.Code, response.Header().Get("Location"))
	}
}

func TestFormsUseRandomBytesOnHTTPAndConfirmOnlyAfterSuccess(t *testing.T) {
	chat := httptest.NewRecorder()
	webPage(chat, httptest.NewRequest(http.MethodGet, "/", nil))
	page := chat.Body.String()
	if strings.Contains(page, "crypto.randomUUID") || !strings.Contains(page, "crypto.getRandomValues(bytes)") || !strings.Contains(page, "newIdempotencyKey()") {
		t.Error("chat does not use HTTP-compatible idempotency keys")
	}
	if !strings.Contains(page, "if(!sent.ok){") || !strings.Contains(page, "message.textContent='Заявка успешно отправлена!'") {
		t.Fatal("chat form success message is missing or shown before server confirmation")
	}
	if !strings.Contains(page, "fetch('/v1/form-resources'") || !strings.Contains(page, "resourceField.addEventListener('change',updateCapabilities)") {
		t.Fatal("access form does not use the live resource and capability catalogue")
	}
	if strings.Contains(page, "JSON.stringify(values,null,2)") || !strings.Contains(page, "Подтвердите отправку заявки.") {
		t.Fatal("access form exposes request JSON or lacks a plain confirmation")
	}
	if !strings.Contains(page, "if(error.status!==409)message.className='error'") {
		t.Fatal("duplicate access request is rendered as a red generic error")
	}
}
