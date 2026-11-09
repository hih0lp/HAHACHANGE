package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func registerJobRoutes(mux *http.ServeMux, jobs *jobManager, apiKey string) {
	mux.Handle("POST /v1/jobs", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		input.Query = strings.TrimSpace(input.Query)
		if len([]rune(input.Query)) < 2 {
			writeError(w, http.StatusBadRequest, errText("query must be at least 2 characters"))
			return
		}
		job, err := jobs.submit(input.Query)
		if errors.Is(err, errQueueFull) {
			writeError(w, http.StatusTooManyRequests, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Location", "/v1/jobs/"+job.ID)
		writeJSON(w, http.StatusAccepted, job)
	})))
	mux.Handle("GET /v1/jobs/{id}", authorize(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		job, ok := jobs.get(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, errText("job not found"))
			return
		}
		writeJSON(w, http.StatusOK, job)
	})))
	mux.HandleFunc("GET /v1/jobs/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := jobs.get(r.PathValue("id")); !ok {
			writeError(w, http.StatusNotFound, errText("job not found"))
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
			if err := conn.ReadJSON(&auth); err != nil || subtle.ConstantTimeCompare([]byte(auth.APIKey), []byte(apiKey)) != 1 {
				return
			}
			_ = conn.SetReadDeadline(time.Time{})
		}
		events, unsubscribe, ok := jobs.subscribe(r.PathValue("id"))
		if !ok {
			return
		}
		defer unsubscribe()
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		for {
			select {
			case <-readDone:
				return
			case event := <-events:
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteJSON(event); err != nil {
					return
				}
				if event.Status == "completed" || event.Status == "failed" {
					return
				}
			}
		}
	})
}
