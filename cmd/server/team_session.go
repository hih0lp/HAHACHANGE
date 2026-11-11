package main

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/hackchange/avito-rag/internal/scenario"
)

// registerTeamSessionRoute gives the demo chat one server-configured identity.
// The password stays in the server environment; only a short-lived JWT reaches
// this browser session.
func registerTeamSessionRoute(mux *http.ServeMux, api scenario.Client) {
	mux.HandleFunc("POST /v1/team-access/session", func(w http.ResponseWriter, r *http.Request) {
		role := demoRole(r)
		username, password := "intern", os.Getenv("TEAM_ACCESS_INTERN_PASSWORD")
		if role == "mentor" {
			username, password = "approver", os.Getenv("TEAM_ACCESS_APPROVER_PASSWORD")
		}
		if password == "" {
			writeError(w, http.StatusServiceUnavailable, errText("team access demo identity is unavailable"))
			return
		}
		result, err := api.Login(r.Context(), username, password)
		if err != nil {
			scenarioError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"access_token": result.AccessToken, "token_type": result.TokenType, "expires_in": result.ExpiresIn, "role": role})
	})
}

func demoRole(r *http.Request) string {
	if c, err := r.Cookie("buddy_demo_role"); err == nil && c.Value == "mentor" {
		return "mentor"
	}
	return "intern"
}

func registerDemoRoleRoute(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/demo/role", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Role string `json:"role"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil || (in.Role != "intern" && in.Role != "mentor") {
			writeError(w, 400, errText("invalid demo role"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "buddy_demo_role", Value: in.Role, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 86400})
		writeJSON(w, 200, map[string]string{"role": in.Role})
	})
}
