package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func TestAccountChatOwnerUsesVerifiedIdentity(t *testing.T) {
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer first":
			writeJSON(w, 200, map[string]string{"id": "intern"})
		case "Bearer second":
			writeJSON(w, 200, map[string]string{"id": "intern2"})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer directory.Close()
	t.Setenv("TEAM_ACCESS_URL", directory.URL)

	request := httptest.NewRequest(http.MethodGet, "/v1/chat/history", nil)
	request = request.WithContext(context.WithValue(request.Context(), ownerKey{}, "browser"))
	if owner, ok := chatOwner(httptest.NewRecorder(), request); !ok || owner != "browser:intern" {
		t.Fatalf("existing demo owner = %q, %v", owner, ok)
	}
	request.Header.Set("X-Bobby-Account", "1")
	for token, want := range map[string]string{"first": "browser:account:intern", "second": "browser:account:intern2"} {
		request.Header.Set("Authorization", "Bearer "+token)
		if owner, ok := chatOwner(httptest.NewRecorder(), request); !ok || owner != want {
			t.Fatalf("account owner = %q, %v, want %q", owner, ok, want)
		}
	}
	request.Header.Set("Authorization", "Bearer invalid")
	response := httptest.NewRecorder()
	if _, ok := chatOwner(response, request); ok || response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid account status = %d", response.Code)
	}
}

func TestFrontendFormsCatalogAndValidation(t *testing.T) {
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/me":
			writeJSON(w, 200, map[string]string{"id": "intern", "current_team_id": "demo"})
		case "/v1/teams/demo/resources":
			writeJSON(w, 200, map[string]any{"resources": []map[string]string{{"id": "jira-demo"}}})
		case "/v1/forms/access/validate":
			var input struct{ Values map[string]string }
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.Values["reason"] != "demo reason" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(w, 200, map[string]any{"valid": true, "version": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer directory.Close()
	t.Setenv("RIGHTS_URL", directory.URL)
	mux := http.NewServeMux()
	registerFormRoutes(mux, "key", directory.URL)
	for _, tc := range []struct {
		method, path, body, key, token, contains string
		want                                     int
	}{
		{"GET", "/v1/forms/resources", "", "key", "session", "jira-demo", 200},
		{"GET", "/v1/forms/resources", "", "key", "", "session", 401},
		{"GET", "/v1/forms/resources", "", "", "session", "API key", 401},
		{"POST", "/v1/forms/access/validate", `{"version":1,"values":{"reason":"demo reason"}}`, "key", "session", `"valid":true`, 200},
		{"POST", "/v1/forms/access/validate", `{"version":2,"values":{"reason":"demo reason"}}`, "key", "session", "version has changed", 409},
		{"POST", "/v1/forms/access/validate", `{"version":1,"values":{}}`, "key", "session", "invalid form values", 400},
		{"POST", "/v1/forms/access/validate", `{}`, "key", "session", "values are required", 400},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("X-API-Key", tc.key)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != tc.want || !strings.Contains(response.Body.String(), tc.contains) {
			t.Errorf("%s %s: status %d, body %s", tc.method, tc.path, response.Code, response.Body.String())
		}
	}
}

func TestFrontendJiraRequiresProfileBinding(t *testing.T) {
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, 200, map[string]string{"id": "intern", "jira_account_id": "demo-jira"})
	}))
	defer directory.Close()
	mux := http.NewServeMux()
	registerPersonalJiraRoute(mux, "", jiraConfig{Mode: "live"}, reminderConfig{InternAccountID: "another-user"}, scenario.Client{BaseURL: directory.URL})
	for _, token := range []string{"session", "unknown", ""} {
		req := httptest.NewRequest(http.MethodGet, "/v1/tasks/jira", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if token == "session" {
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) {
				t.Fatalf("unbound Jira status %d, body %s", response.Code, response.Body.String())
			}
		} else if response.Code != http.StatusUnauthorized {
			t.Errorf("unauthorized Jira status %d", response.Code)
		}
	}
}
