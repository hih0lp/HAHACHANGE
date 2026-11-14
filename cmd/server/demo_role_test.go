package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoRoleSwitch(t *testing.T) {
	mux := http.NewServeMux()
	registerDemoRoleRoute(mux)
	request := httptest.NewRequest(http.MethodPost, "/v1/demo/role", strings.NewReader(`{"role":"mentor"}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 200 || len(response.Result().Cookies()) != 1 {
		t.Fatalf("switch: %d %s", response.Code, response.Body.String())
	}
	read := httptest.NewRequest(http.MethodGet, "/", nil)
	read.AddCookie(response.Result().Cookies()[0])
	if demoRole(read) != "mentor" {
		t.Fatal("mentor role not retained")
	}
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/v1/demo/role", strings.NewReader(`{"role":"admin"}`)))
	if bad.Code != 400 {
		t.Fatalf("unexpected role accepted: %d", bad.Code)
	}
}

func TestDemoBootstrapRenewsSelectedRole(t *testing.T) {
	handler := demoSession("test-secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "buddy_demo_role", Value: "intern2"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "buddy_demo_role" && cookie.Value == "intern2" && cookie.MaxAge == demoSessionMaxAge {
			return
		}
	}
	t.Fatal("bootstrap did not renew the selected role")
}
