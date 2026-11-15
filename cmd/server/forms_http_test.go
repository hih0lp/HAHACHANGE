package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormGatewayDistinguishesInvalidInputAndUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := http.StatusServiceUnavailable
		switch r.URL.Path {
		case "/v1/me":
			if r.Header.Get("Authorization") == "Bearer valid" {
				w.Write([]byte(`{"current_team_id":"demo-team"}`))
				return
			}
		case "/v1/forms/sample/validate":
			switch r.Header.Get("Authorization") {
			case "Bearer invalid":
				code = http.StatusBadRequest
			case "Bearer expired":
				code = http.StatusUnauthorized
			}
		case "/v1/access/check":
			if r.Header.Get("Authorization") == "Bearer granted" {
				w.Write([]byte(`{"state":"granted"}`))
				return
			}
		}
		w.WriteHeader(code)
		w.Write([]byte(`{"error":{"code":"unavailable","message":"backend failure"}}`))
	}))
	defer upstream.Close()
	t.Setenv("RIGHTS_URL", upstream.URL)
	mux := http.NewServeMux()
	registerFormRoutes(mux, "", upstream.URL)
	for _, tc := range []struct {
		m, path, body, token string
		status               int
	}{
		{"GET", "/v1/forms", "", "", 401},
		{"GET", "/v1/form-resources", "", "", 401},
		{"GET", "/v1/forms/sample", "", "", 401},
		{"GET", "/v1/forms/sample/access-request", "", "", 401},
		{"POST", "/v1/forms/sample/validate", "{}", "", 401},
		{"POST", "/v1/forms/sample/submit", "{}", "", 401},
		{"GET", "/v1/forms", "", "valid", 503},
		{"GET", "/v1/forms/sample", "", "valid", 503},
		{"GET", "/v1/forms/sample/access-request", "", "valid", 503},
		{"GET", "/v1/form-resources", "", "expired", 503},
		{"GET", "/v1/form-resources", "", "valid", 503},
		{"POST", "/v1/forms/sample/validate", "{}", "valid", 400},
		{"POST", "/v1/forms/sample/validate", `{"version":1,"values":{}}`, "invalid", 400},
		{"POST", "/v1/forms/sample/validate", `{"version":1,"values":{}}`, "expired", 401},
		{"POST", "/v1/forms/sample/validate", `{"version":1,"values":{}}`, "valid", 503},
		{"POST", "/v1/forms/sample/submit", `{"confirmed":false}`, "valid", 400},
		{"POST", "/v1/forms/sample/submit", `{"confirmed":true,"values":{"resource_id":"doc","capability":"read"}}`, "granted", 409},
		{"POST", "/v1/forms/sample/submit", `{"confirmed":true,"values":{}}`, "valid", 400},
	} {
		req := httptest.NewRequest(tc.m, tc.path, strings.NewReader(tc.body))
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		got := httptest.NewRecorder()
		mux.ServeHTTP(got, req)
		if got.Code != tc.status {
			t.Errorf("%s %s token=%s: %d want %d (%s)", tc.m, tc.path, tc.token, got.Code, tc.status, got.Body)
		}
	}
}

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

func TestFormValidationProxyChecksSessionValuesAndVersion(t *testing.T) {
	rights := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/forms/equipment-request/validate" || r.Header.Get("Authorization") != "Bearer test-session" {
			t.Errorf("unexpected validation request: %s %s", r.Method, r.URL)
		}
		var input struct {
			Values map[string]any `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Values["item"] != "Ноутбук" || input.Values["reason"] != "Для работы" {
			t.Errorf("values=%v", input.Values)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true,"version":1,"form_id":"equipment-request"}`))
	}))
	defer rights.Close()
	t.Setenv("RIGHTS_URL", rights.URL)
	mux := http.NewServeMux()
	registerFormRoutes(mux, "", rights.URL)
	request := func(body string, token bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/forms/equipment-request/validate", strings.NewReader(body))
		if token {
			req.Header.Set("Authorization", "Bearer test-session")
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		return response
	}
	if response := request(`{"version":1,"values":{"item":"Ноутбук","reason":"Для работы"}}`, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing session: %d", response.Code)
	}
	if response := request(`{"version":1,"values":{"item":"Ноутбук","reason":"Для работы"}}`, true); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"valid":true`) {
		t.Fatalf("valid form: %d %s", response.Code, response.Body.String())
	}
	if response := request(`{"version":2,"values":{"item":"Ноутбук","reason":"Для работы"}}`, true); response.Code != http.StatusConflict {
		t.Fatalf("stale version: %d %s", response.Code, response.Body.String())
	}
	if response := request(`{"version":1}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("missing values: %d %s", response.Code, response.Body.String())
	}
}

func TestAccessFormRestoresSavedReceipt(t *testing.T) {
	rights := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/access-requests" || r.URL.Query().Get("form_id") != "test-form" ||
			r.Header.Get("Authorization") != "Bearer test-session" {
			t.Errorf("unexpected receipt request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"requests":[{"id":"saved","status":"approved"}]}`))
	}))
	defer rights.Close()
	t.Setenv("RIGHTS_URL", rights.URL)
	mux := http.NewServeMux()
	registerFormRoutes(mux, "", rights.URL)
	req := httptest.NewRequest(http.MethodGet, "/v1/forms/test-form/access-request", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("without token: %d", response.Code)
	}
	req.Header.Set("Authorization", "Bearer test-session")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"saved"`) ||
		!strings.Contains(response.Body.String(), `"status":"approved"`) {
		t.Fatalf("receipt: %d %s", response.Code, response.Body.String())
	}
}
