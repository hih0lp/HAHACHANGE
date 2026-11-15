package scenario

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientUsesSeparateDirectoryAndRightsEndpoints(t *testing.T) {
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/auth/login":
			w.Write([]byte(`{"access_token":"signed","token_type":"Bearer","expires_in":900}`))
		case "/v1/teams/team-a/members":
			if r.Header.Get("Authorization") != "Bearer signed" {
				t.Error("missing directory JWT")
			}
			w.Write([]byte(`{"members":[{"id":"mentor","display_name":"Mentor"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer directory.Close()
	rights := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer signed" {
			t.Error("missing rights JWT")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/forms":
			w.Write([]byte(`{"forms":[{"id":"access-request","title":"Access"}]}`))
		case "/v1/forms/access-request":
			w.Write([]byte(`{"id":"access-request","title":"Access"}`))
		case "/v1/forms/access-request/validate":
			w.Write([]byte(`{"valid":true}`))
		case "/v1/forms/access-request/submit":
			if r.Header.Get("Idempotency-Key") != "stable-key" {
				t.Error("missing idempotency key")
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["confirmed"] != true {
				t.Error("submission lacked confirmation")
			}
			w.Write([]byte(`{"id":"submission"}`))
		case "/v1/access-requests":
			w.Write([]byte(`{"requests":[{"id":"request-1","status":"pending"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer rights.Close()
	client := Client{BaseURL: directory.URL, RightsURL: rights.URL}
	ctx := context.Background()
	if login, err := client.Login(ctx, "intern", "secret"); err != nil || login.AccessToken != "signed" {
		t.Fatalf("login=%+v err=%v", login, err)
	}
	if members, err := client.Members(ctx, "signed", "team-a"); err != nil || len(members) != 1 || members[0].ID != "mentor" {
		t.Fatalf("members=%+v err=%v", members, err)
	}
	if forms, err := client.Forms(ctx, "signed"); err != nil || len(forms) != 1 {
		t.Fatalf("forms=%+v err=%v", forms, err)
	}
	if form, err := client.Form(ctx, "signed", "access-request"); err != nil || form.Title != "Access" {
		t.Fatalf("form=%+v err=%v", form, err)
	}
	if result, err := client.ValidateForm(ctx, "signed", "access-request", map[string]any{"reason": "Need access"}); err != nil || result["valid"] != true {
		t.Fatalf("validation=%+v err=%v", result, err)
	}
	if result, err := client.SubmitForm(ctx, "signed", "stable-key", "access-request", 1, map[string]any{"reason": "Need access"}); err != nil || result["id"] != "submission" {
		t.Fatalf("submission=%+v err=%v", result, err)
	}
	if requests, err := client.FormAccessRequests(ctx, "signed", "access-request"); err != nil || len(requests) != 1 || requests[0].ID != "request-1" {
		t.Fatalf("requests=%+v err=%v", requests, err)
	}
	if _, err := client.Form(ctx, "signed", "missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing form error=%v", err)
	}
}
