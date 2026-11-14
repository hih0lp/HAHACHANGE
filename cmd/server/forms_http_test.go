package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormResourcesComeFromCurrentTeam(t *testing.T) {
	team := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected team request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"id":"test-user","current_team_id":"test-team"}`))
	}))
	defer team.Close()
	rights := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/teams/test-team/resources" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected rights request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"resources":[{"id":"resource-one","name":"Ресурс","capabilities":["read"]}]}`))
	}))
	defer rights.Close()
	t.Setenv("RIGHTS_URL", rights.URL)
	mux := http.NewServeMux()
	registerFormRoutes(mux, "", team.URL)
	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/form-resources", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("without bearer: %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/form-resources", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "resource-one") || !strings.Contains(response.Body.String(), "read") {
		t.Fatalf("resource catalogue: %d %s", response.Code, response.Body.String())
	}
}

func TestAccessFormReportsAlreadyPendingRequest(t *testing.T) {
	rights := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/access/check" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"pending"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer rights.Close()
	t.Setenv("RIGHTS_URL", rights.URL)
	mux := http.NewServeMux()
	registerFormRoutes(mux, "", rights.URL)
	request := httptest.NewRequest(http.MethodPost, "/v1/forms/access-request/submit", strings.NewReader(`{"version":1,"values":{"resource_id":"resource-from-schema","capability":"capability-from-schema"},"confirmed":true}`))
	request.Header.Set("Authorization", "Bearer test-session")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Заявка на этот доступ уже отправлена и ожидает решения.") {
		t.Fatalf("status %d, body %s", response.Code, response.Body.String())
	}
}
