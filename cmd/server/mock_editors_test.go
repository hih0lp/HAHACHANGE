package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockItemPutProxiesBearerAndPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/v1/mock-data/gitlab/projects/2/branches/docs/demo" || r.Header.Get("Authorization") != "Bearer intern" {
			t.Errorf("unexpected upstream request: %s %s %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":2}`))
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	registerMockEditors(mux, "", map[string]string{"gitlab": upstream.URL}, "")
	r := httptest.NewRequest("PUT", "/v1/mock-data/gitlab/projects/2/branches/docs/demo", strings.NewReader(`{"name":"docs/new"}`))
	r.Header.Set("Authorization", "Bearer intern")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":2`) {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
}
