package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func TestTeamAccessContextUsesVerifiedServiceResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			_, _ = w.Write([]byte(`{"id":"intern","current_team_id":"team","teams":[{"id":"team","name":"Team"}]}`))
		case "/v1/teams/team/resources":
			_, _ = w.Write([]byte(`{"resources":[{"id":"repo","kind":"gitlab_project","name":"Repo","capabilities":["read_repository"]}]}`))
		case "/v1/access/check":
			_, _ = w.Write([]byte(`{"state":"missing","resource_id":"repo","capability":"read_repository","checked_at":"2026-11-10T00:00:00Z"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	data, err := teamAccessContext(context.Background(), scenario.Client{BaseURL: server.URL}, "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-token") {
		t.Fatal("JWT leaked into stored context")
	}
	var got struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
		Checks []struct {
			State string `json:"state"`
		} `json:"checks"`
	}
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Profile.ID != "intern" || len(got.Checks) != 1 || got.Checks[0].State != "missing" {
		t.Fatalf("unexpected context: %s", data)
	}
	if _, err = teamAccessContext(context.Background(), scenario.Client{BaseURL: server.URL}, "bad-token"); err == nil {
		t.Fatal("invalid token accepted")
	}
}
