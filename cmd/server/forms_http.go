package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func registerFormRoutes(mux *http.ServeMux, key string, teamURL string) {
	client := scenario.Client{BaseURL: teamURL, RightsURL: os.Getenv("RIGHTS_URL")}
	withToken := func(w http.ResponseWriter, r *http.Request) string {
		bearer := r.Header.Get("Authorization")
		if !strings.HasPrefix(bearer, "Bearer ") {
			writeError(w, 401, errText("team session required"))
			return ""
		}
		return strings.TrimPrefix(bearer, "Bearer ")
	}
	mux.Handle("GET /v1/forms", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := withToken(w, r)
		if token == "" {
			return
		}
		v, err := client.Forms(r.Context(), token)
		if err != nil {
			writeError(w, 503, errText("forms unavailable"))
			return
		}
		writeJSON(w, 200, map[string]any{"forms": v})
	})))
	mux.Handle("GET /v1/forms/{id}", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := withToken(w, r)
		if token == "" {
			return
		}
		v, err := client.Form(r.Context(), token, r.PathValue("id"))
		if err != nil {
			writeError(w, 503, errText("form unavailable"))
			return
		}
		writeJSON(w, 200, v)
	})))
	mux.Handle("POST /v1/forms/{id}/submit", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := withToken(w, r)
		if token == "" {
			return
		}
		var in struct {
			Version   int            `json:"version"`
			Values    map[string]any `json:"values"`
			Confirmed bool           `json:"confirmed"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || !in.Confirmed {
			writeError(w, 400, errText("confirmation required"))
			return
		}
		v, err := client.SubmitForm(r.Context(), token, r.Header.Get("Idempotency-Key"), r.PathValue("id"), in.Version, in.Values)
		if err != nil {
			writeError(w, 400, errText("form could not be submitted: verify fields and access"))
			return
		}
		writeJSON(w, 201, v)
	})))
	mux.HandleFunc("GET /forms/{id}", formPage)
}
