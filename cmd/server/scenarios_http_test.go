package main

import (
	"github.com/hackchange/avito-rag/internal/scenario"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScenarioRequiresVerifiedIdentity(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "invalid token", 401) }))
	defer service.Close()
	mux := http.NewServeMux()
	registerScenarioRoutes(mux, scenario.Store{}, scenario.Client{BaseURL: service.URL})
	for _, token := range []string{"", "Bearer forged"} {
		req := httptest.NewRequest("POST", "/v1/scenarios", strings.NewReader(`{}`))
		req.Header.Set("Authorization", token)
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		if out.Code != 401 {
			t.Fatalf("unverified identity reached scenario storage: %d", out.Code)
		}
	}
}
func TestScenarioLoginDoesNotCacheCredentials(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/login" {
			t.Error("wrong login path")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"signed-token","token_type":"Bearer","expires_in":900}`))
	}))
	defer service.Close()
	mux := http.NewServeMux()
	registerScenarioRoutes(mux, scenario.Store{}, scenario.Client{BaseURL: service.URL})
	req := httptest.NewRequest("POST", "/v1/scenarios/login", strings.NewReader(`{"username":"intern","password":"test"}`))
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, req)
	if out.Code != 200 || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe login response: %d", out.Code)
	}
}
