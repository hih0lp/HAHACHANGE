package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type accessRequest struct {
	ID             string    `json:"id"`
	Resource       string    `json:"resource"`
	Reason         string    `json:"reason"`
	Key            string    `json:"key"`
	Status         string    `json:"status"`
	URL            string    `json:"url,omitempty"`
	Mode           string    `json:"mode"`
	CreatedAt      time.Time `json:"created_at"`
	NextStep       string    `json:"next_step"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}
type accessStore struct {
	sync.Mutex
	path    string
	records []accessRequest
}

func loadAccessStore(path string) (*accessStore, error) {
	s := &accessStore{path: path, records: []accessRequest{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &s.records); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *accessStore) save(record accessRequest) error {
	s.records = append(s.records, record)
	committed := false
	defer func() {
		if !committed {
			s.records = s.records[:len(s.records)-1]
		}
	}()
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".access-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	committed = true
	return nil
}
func registerAccessRoutes(mux *http.ServeMux, key string, cfg jiraConfig) {
	if cfg.Mode == "off" {
		return
	}
	store, err := loadAccessStore(cfg.DataPath)
	if err != nil { // Keep the API available, but writes report the store error.
		mux.Handle("/v1/access-requests", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeError(w, 500, err) })))
		mux.Handle("/v1/access-requests/", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeError(w, 500, err) })))
		return
	}
	registerAccessRoutesWithStore(mux, key, cfg, store, jiraTracker{config: cfg})
}
func registerAccessRoutesWithStore(mux *http.ServeMux, key string, cfg jiraConfig, store *accessStore, tracker issueTracker) {
	mux.Handle("POST /v1/access-requests", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Resource  string `json:"resource"`
			Reason    string `json:"reason"`
			Confirmed bool   `json:"confirmed"`
			Role      string `json:"role"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil {
			writeError(w, 400, err)
			return
		}
		input.Resource = strings.TrimSpace(input.Resource)
		input.Reason = strings.TrimSpace(input.Reason)
		if !input.Confirmed {
			writeError(w, 400, errText("explicit confirmation is required"))
			return
		}
		if input.Role != "intern" {
			writeError(w, 403, errText("demo intern role is required"))
			return
		}
		if len([]rune(input.Resource)) < 2 || len([]rune(input.Resource)) > 120 || len([]rune(input.Reason)) < 2 || len([]rune(input.Reason)) > 500 {
			writeError(w, 400, errText("resource or reason length is invalid"))
			return
		}
		idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if len(idem) < 8 || len(idem) > 128 {
			writeError(w, 400, errText("Idempotency-Key must be 8–128 characters"))
			return
		}
		store.Lock()
		defer store.Unlock()
		for _, record := range store.records {
			if record.IdempotencyKey == idem {
				if record.Resource != input.Resource || record.Reason != input.Reason {
					writeError(w, 409, errText("idempotency key was used for different input"))
					return
				}
				writeJSON(w, 200, record)
				return
			}
		}
		issue, err := tracker.CreateIssue(r.Context(), issueInput{input.Resource, input.Reason})
		if err != nil {
			writeError(w, 502, err)
			return
		}
		idBytes := make([]byte, 16)
		if _, err = rand.Read(idBytes); err != nil {
			writeError(w, 500, err)
			return
		}
		record := accessRequest{ID: hex.EncodeToString(idBytes), Resource: input.Resource, Reason: input.Reason, Key: issue.Key, Status: issue.Status, URL: issue.URL, Mode: cfg.Mode, CreatedAt: time.Now().UTC(), NextStep: "Откройте задачу и дождитесь ответа ответственного за доступ.", IdempotencyKey: idem}
		if err = store.save(record); err != nil {
			writeError(w, 500, err)
			return
		}
		writeJSON(w, 201, record)
	})))
	mux.Handle("GET /v1/access-requests", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store.Lock()
		defer store.Unlock()
		records := make([]accessRequest, len(store.records))
		for i, record := range store.records {
			records[len(records)-i-1] = record
		}
		writeJSON(w, 200, map[string]any{"requests": records})
	})))
	mux.Handle("GET /v1/access-requests/{id}", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store.Lock()
		defer store.Unlock()
		for _, record := range store.records {
			if record.ID == r.PathValue("id") {
				writeJSON(w, 200, record)
				return
			}
		}
		writeError(w, 404, errText("request not found"))
	})))
}
