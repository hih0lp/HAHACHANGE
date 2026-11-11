package teamaccess

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormSchemaValidation(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"item":{"type":"string","minLength":2},"count":{"type":"integer","minimum":1}},"required":["item","count"],"additionalProperties":false}`)
	if err := validateForm(schema, map[string]any{"item": "Ноутбук", "count": float64(1)}); err != nil {
		t.Fatal(err)
	}
	for _, values := range []map[string]any{
		{"item": "x", "count": float64(1)},
		{"item": "Ноутбук"},
		{"item": "Ноутбук", "count": float64(0)},
		{"item": "Ноутбук", "count": float64(1), "unexpected": true},
	} {
		if err := validateForm(schema, values); err == nil {
			t.Fatalf("invalid form accepted: %v", values)
		}
	}
}

func TestServiceRoutesAreSeparated(t *testing.T) {
	team := (&Service{role: "team"}).Routes()
	rights := (&Service{role: "rights"}).Routes()
	check := func(handler http.Handler, path string, want int) {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("GET %s: got %d want %d", path, w.Code, want)
		}
	}
	check(team, "/v1/forms", 404)
	check(rights, "/v1/me", 404)
	check(rights, "/v1/forms", 401)
}
