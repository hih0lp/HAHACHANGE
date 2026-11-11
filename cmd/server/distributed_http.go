package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/jackc/pgx/v5"
)

type ownerKey struct{}

func demoSession(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalJiraMCP := r.URL.Path == "/mcp"
		var owner string
		if cookie, err := r.Cookie("ai_demo_session"); err == nil {
			parts := strings.Split(cookie.Value, ".")
			if len(parts) == 2 && len(parts[0]) == 36 {
				mac := hmac.New(sha256.New, []byte(secret))
				mac.Write([]byte(parts[0]))
				if expected, err := hex.DecodeString(parts[1]); err == nil && hmac.Equal(expected, mac.Sum(nil)) {
					owner = parts[0]
				}
			}
		}
		if owner == "" && r.Method == http.MethodGet && (r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/forms/")) {
			id, err := distributed.NewID()
			if err != nil {
				writeError(w, 500, err)
				return
			}
			owner = id
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(id))
			http.SetCookie(w, &http.Cookie{Name: "ai_demo_session", Value: id + "." + hex.EncodeToString(mac.Sum(nil)), Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 86400})
		}
		if owner == "" && !internalJiraMCP {
			writeError(w, 401, errText("open the application to start a demo session"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, owner)))
	})
}

func ownerFrom(r *http.Request) string {
	owner, _ := r.Context().Value(ownerKey{}).(string)
	return owner
}

func registerDistributedJobRoutes(mux *http.ServeMux, jobs *distributed.System, apiKey string) {
	mux.Handle("POST /v1/jobs", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct{ Query, Scenario string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeError(w, 400, err)
			return
		}
		input.Query = strings.TrimSpace(input.Query)
		if len([]rune(input.Query)) < 2 {
			writeError(w, 400, errText("query must be at least 2 characters"))
			return
		}
		if input.Scenario == "" {
			input.Scenario = "general"
		}
		switch input.Scenario {
		case "general", "first-week", "access", "absence":
		default:
			writeError(w, 400, errText("unknown scenario"))
			return
		}
		teamToken := ""
		if endpoint := os.Getenv("TEAM_ACCESS_URL"); endpoint != "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				teamToken = strings.TrimPrefix(auth, "Bearer ")
				// Validate the credential here, but do not prefetch team data. The
				// model chooses read-only tools later in the worker.
				if _, err := (scenario.Client{BaseURL: endpoint}).Profile(r.Context(), teamToken); err != nil {
					writeError(w, 503, errText("team access verification failed"))
					return
				}
			}
		}
		job, err := jobs.Submit(r.Context(), ownerFrom(r), input.Scenario, input.Query)
		if err != nil {
			writeError(w, 503, errText("request storage is unavailable"))
			return
		}
		if err = jobs.StoreTeamToken(r.Context(), job.ID, teamToken); err != nil {
			writeError(w, 503, errText("team access tools are temporarily unavailable"))
			return
		}
		w.Header().Set("Location", "/v1/jobs/"+job.ID)
		writeJSON(w, 202, job)
	})))
	mux.Handle("GET /v1/jobs/{id}", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		job, err := jobs.Get(r.Context(), ownerFrom(r), r.PathValue("id"))
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, errText("job not found"))
			return
		}
		if err != nil {
			writeError(w, 503, errText("request storage is unavailable"))
			return
		}
		writeJSON(w, 200, job)
	})))
	mux.HandleFunc("GET /v1/jobs/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		id, owner := r.PathValue("id"), ownerFrom(r)
		job, err := jobs.Get(r.Context(), owner, id)
		if err != nil {
			writeError(w, 404, errText("job not found"))
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if apiKey != "" {
			conn.SetReadLimit(2048)
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var auth struct {
				APIKey string `json:"api_key"`
			}
			if conn.ReadJSON(&auth) != nil || subtle.ConstantTimeCompare([]byte(auth.APIKey), []byte(apiKey)) != 1 {
				return
			}
		}
		if conn.WriteJSON(job) != nil || job.Status == "completed" || job.Status == "failed" {
			return
		}
		closed := make(chan struct{})
		go func() {
			defer close(closed)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-closed:
				return
			case <-r.Context().Done():
				return
			case <-ticker.C:
				current, err := jobs.Get(r.Context(), owner, id)
				if err != nil {
					return
				}
				if current.Status == job.Status {
					continue
				}
				job = current
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if conn.WriteJSON(job) != nil || job.Status == "completed" || job.Status == "failed" {
					return
				}
			}
		}
	})
}
