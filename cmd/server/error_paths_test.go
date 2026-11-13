package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAskRejectsInvalidLLMResponses(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"upstream failure", `{"error":"unavailable"}`, http.StatusServiceUnavailable},
		{"invalid JSON", `{`, http.StatusOK},
		{"missing choice", `{"choices":[]}`, http.StatusOK},
		{"blank answer", `{"choices":[{"message":{"content":"  "}}]}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer llm.Close()
			t.Setenv("LLM_URL", llm.URL)
			response := call(testHandler(t, ""), http.MethodPost, "/v1/ask", `{"query":"Как проходит ревью?"}`, "")
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAskReportsLLMConnectionFailure(t *testing.T) {
	t.Setenv("LLM_URL", "http://127.0.0.1:1")
	response := call(testHandler(t, ""), http.MethodPost, "/v1/ask", `{"query":"Как проходит ревью?"}`, "")
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestJiraConfigValidation(t *testing.T) {
	for _, cfg := range []jiraConfig{
		{Mode: "unexpected"},
		{Mode: "live", BaseURL: "http://example.org"},
		{Mode: "live", BaseURL: "https://example.org"},
	} {
		if err := cfg.validate(); err == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	if err := (jiraConfig{Mode: "off"}).validate(); err != nil {
		t.Fatal(err)
	}
}
