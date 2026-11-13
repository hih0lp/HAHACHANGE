package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReactRoutesAndAssets(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BOBBY_WEB_ROOT", root)
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{
		"index.html":     `<div id="root"></div><script type="module" src="/assets/main.js"></script>`,
		"assets/main.js": "console.log('Bobby');",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	handler := demoSession("test-session-secret", newHandler(nil, config{}))
	for _, path := range []string{"/", "/onboarding", "/tasks"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `id="root"`) {
			t.Fatalf("%s: status %d, body %q", path, response.Code, response.Body.String())
		}
		if len(response.Result().Cookies()) != 1 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s did not bootstrap the demo session", path)
		}
		asset := httptest.NewRequest(http.MethodGet, "/assets/main.js", nil)
		asset.AddCookie(response.Result().Cookies()[0])
		assetResponse := httptest.NewRecorder()
		handler.ServeHTTP(assetResponse, asset)
		if assetResponse.Code != http.StatusOK || !strings.Contains(assetResponse.Body.String(), "Bobby") {
			t.Fatalf("asset: status %d, body %q", assetResponse.Code, assetResponse.Body.String())
		}
	}
}

func TestWebPageReportsMissingBuild(t *testing.T) {
	t.Setenv("BOBBY_WEB_ROOT", t.TempDir())
	response := httptest.NewRecorder()
	webPage(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
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
