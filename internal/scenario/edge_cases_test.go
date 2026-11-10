package scenario

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIncompleteTeamContext(t *testing.T) {
	for _, tc := range []struct {
		name      string
		profile   Profile
		resources []Resource
	}{
		{"no team", Profile{ID: "u"}, nil},
		{"team without Jira", Profile{ID: "u", Teams: []Team{{ID: "t"}}}, []Resource{{ID: "repo", Kind: "gitlab_project"}}},
		{"ambiguous Jira", Profile{ID: "u", Teams: []Team{{ID: "t"}}}, []Resource{{ID: "a", Kind: "jira_project"}, {ID: "b", Kind: "jira_project"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/access/check" {
					t.Error("checked permissions without unambiguous resource")
				}
				json.NewEncoder(w).Encode(map[string]any{"resources": tc.resources})
			}))
			defer server.Close()
			state, err := Advance(context.Background(), Client{BaseURL: server.URL}, "token", State{OwnerID: "u"}, tc.profile)
			if err != nil || state.Status != "needs_input" || state.Widget.Visible {
				t.Fatalf("%+v %v", state, err)
			}
		})
	}
}

func TestCheckContractAndRefresh(t *testing.T) {
	for _, tc := range []struct {
		state, id, cap string
		valid          bool
	}{{"granted", "jira", "browse_issues", true}, {"unsupported", "jira", "browse_issues", false}, {"granted", "other", "browse_issues", false}, {"granted", "jira", "admin", false}, {"outage", "jira", "browse_issues", false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/access/check" {
				if tc.state == "outage" {
					http.Error(w, "down", 503)
					return
				}
				json.NewEncoder(w).Encode(Check{State: tc.state, ResourceID: tc.id, Capability: tc.cap})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"resources": []Resource{{ID: "jira", Kind: "jira_project"}}})
		}))
		s, err := Advance(context.Background(), Client{BaseURL: server.URL}, "token", State{OwnerID: "u", Checks: []Check{{ResourceID: "jira", Capability: "browse_issues", State: "pending"}}}, Profile{ID: "u", Teams: []Team{{ID: "t"}}})
		server.Close()
		if tc.valid {
			if err != nil || len(s.Checks) != 1 || s.Checks[0].State != "granted" {
				t.Fatalf("stale check was not replaced %+v %v", s, err)
			}
		} else if err == nil {
			t.Fatal("invalid check accepted")
		}
	}
}

func TestClientRejectsInvalidResponsesAndRedirects(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++; w.Write([]byte(`{}`)) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "redirect":
			http.Redirect(w, r, destination.URL, 302)
		case "bad":
			w.Write([]byte(`{`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	c := Client{BaseURL: server.URL}
	if _, err := c.Profile(context.Background(), "secret"); err == nil {
		t.Fatal("empty user profile accepted")
	}
	var out map[string]any
	for _, path := range []string{"/?mode=redirect", "/?mode=bad"} {
		if err := c.call(context.Background(), "secret", "GET", path, "", nil, &out); err == nil {
			t.Fatal("bad response accepted")
		}
	}
	if destinationCalls != 0 {
		t.Fatal("token followed redirect")
	}
	if c.call(context.Background(), "secret", "GET", "/", "", make(chan int), &out) == nil {
		t.Fatal("bad input accepted")
	}
	c.BaseURL = "://bad"
	if c.call(context.Background(), "secret", "GET", "/", "", nil, &out) == nil {
		t.Fatal("bad URL accepted")
	}
	c.BaseURL = "http://127.0.0.1:1"
	if c.call(context.Background(), "secret", "GET", "/", "", nil, &out) == nil {
		t.Fatal("network failure ignored")
	}
}
