package teamaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestDirectoryAndRightsFlow(t *testing.T) {
	ctx := context.Background()
	_, source, _, _ := runtime.Caller(0)
	migrations := filepath.Join(filepath.Dir(source), "../../migrations/team_access")
	keyFile := filepath.Join(t.TempDir(), "team.key")
	t.Setenv("TEAM_ACCESS_KEY_FILE", keyFile)
	t.Setenv("TEAM_ACCESS_INTERN_PASSWORD", "integration-intern-password")
	t.Setenv("TEAM_ACCESS_INTERN2_PASSWORD", "integration-intern2-password")
	t.Setenv("TEAM_ACCESS_APPROVER_PASSWORD", "integration-approver-password")
	t.Setenv("TEAM_ACCESS_ROLE", "team")
	t.Setenv("TEAM_ACCESS_MIGRATION", filepath.Join(migrations, "001_team.sql"))
	team, err := New(ctx, testsupport.Postgres(t))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(team.Routes())
	defer server.Close()
	client := server.Client()
	login := func(username, password string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		resp, err := client.Post(server.URL+"/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("login %s: %d", username, resp.StatusCode)
		}
		var out struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out.AccessToken
	}
	intern := login("intern", "integration-intern-password")
	other := login("intern2", "integration-intern2-password")
	approver := login("approver", "integration-approver-password")
	call := func(base, method, path, token, body string, want int, headers map[string]string) map[string]any {
		t.Helper()
		req, err := http.NewRequest(method, base+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: status %d, want %d: %v", method, path, resp.StatusCode, want, out)
		}
		return out
	}
	profile := call(server.URL, "GET", "/v1/me", intern, "", 200, nil)
	if profile["id"] != "intern" {
		t.Fatalf("wrong profile: %v", profile)
	}
	call(server.URL, "GET", "/v1/teams/demo-team/members", intern, "", 200, nil)
	call(server.URL, "GET", "/v1/teams/search-team/members", intern, "", 404, nil)
	call(server.URL, "GET", "/v1/teams/search-team/members", other, "", 200, nil)

	seed := filepath.Join(t.TempDir(), "resources.json")
	fixture := `{"resources":[{"id":"demo-calendar","team_id":"demo-team","kind":"calendar","name":"Calendar","external_ref":"demo","url":"https://example.test/calendar","capabilities":["read"],"approver_id":"approver"}]}`
	if err := os.WriteFile(seed, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAM_ACCESS_ROLE", "rights")
	t.Setenv("TEAM_ACCESS_MIGRATION", filepath.Join(migrations, "001_rights.sql"))
	t.Setenv("TEAM_ACCESS_SEED_RESOURCES_FILE", seed)
	t.Setenv("TEAM_DIRECTORY_URL", server.URL)
	rights, err := New(ctx, testsupport.Postgres(t))
	if err != nil {
		t.Fatal(err)
	}
	rightsServer := httptest.NewServer(rights.Routes())
	defer rightsServer.Close()
	base := rightsServer.URL
	call(base, "POST", "/v1/access/check", intern, `{}`, 400, nil)
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":`, 400, nil)
	call(base, "POST", "/v1/access/check", intern, `{}{}`, 400, nil)
	call(base, "GET", "/v1/teams/demo-team/resources", intern, "", 200, nil)
	call(base, "GET", "/v1/teams/search-team/resources", intern, "", 404, nil)
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-calendar","capability":"read"}`, 200, nil)
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-calendar","capability":"write"}`, 400, nil)
	request := `{"resource_id":"demo-calendar","capability":"read","reason":"Need calendar","confirmed":true}`
	call(base, "POST", "/v1/access-requests", approver, request, 403, map[string]string{"Idempotency-Key": "cannot-approve-own"})
	created := call(base, "POST", "/v1/access-requests", intern, request, 201, map[string]string{"Idempotency-Key": "integration-key-1"})
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("missing request ID: %v", created)
	}
	call(base, "POST", "/v1/access-requests", intern, request, 200, map[string]string{"Idempotency-Key": "integration-key-1"})
	call(base, "POST", "/v1/access-requests", intern, request, 409, map[string]string{"Idempotency-Key": "duplicate-pending"})
	call(base, "GET", "/v1/access-requests/"+id, other, "", 404, nil)
	call(base, "GET", "/v1/access-requests", intern, "", 200, nil)
	call(base, "GET", "/v1/approvals", approver, "", 200, nil)
	call(base, "POST", "/v1/access-requests/"+id+"/decision", intern, `{"decision":"approve"}`, 404, nil)
	call(base, "POST", "/v1/access-requests/"+id+"/decision", approver, `{"decision":"approve","comment":"`+strings.Repeat("x", 1001)+`"}`, 400, nil)
	call(base, "POST", "/v1/access-requests/"+id+"/decision", approver, `{"decision":"approve"}`, 200, nil)
	status := call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-calendar","capability":"read"}`, 200, nil)
	if status["state"] != "granted" {
		t.Fatalf("grant not visible: %v", status)
	}
	call(base, "POST", "/v1/access-requests/"+id+"/decision", approver, `{"decision":"approve"}`, 422, nil)

	forms := call(base, "GET", "/v1/forms", intern, "", 200, nil)
	if len(forms["forms"].([]any)) < 2 {
		t.Fatalf("built-in forms missing: %v", forms)
	}
	call(base, "GET", "/v1/forms/equipment-request", intern, "", 200, nil)
	call(base, "GET", "/v1/forms/unknown", intern, "", 404, nil)
	valid := `{"values":{"item":"Laptop","reason":"Development"}}`
	call(base, "POST", "/v1/forms/equipment-request/validate", intern, valid, 200, nil)
	call(base, "POST", "/v1/forms/equipment-request/validate", intern, `{"values":{"item":"x"}}`, 400, nil)
	submission := `{"values":{"item":"Laptop","reason":"Development"},"confirmed":true,"version":1}`
	call(base, "POST", "/v1/forms/equipment-request/submit", intern, submission, 400, map[string]string{"Idempotency-Key": "short"})
	call(base, "POST", "/v1/forms/equipment-request/submit", intern, `{"values":{},"confirmed":true,"version":1}`, 400, map[string]string{"Idempotency-Key": "invalid-fields"})
	formResult := call(base, "POST", "/v1/forms/equipment-request/submit", intern, submission, 201, map[string]string{"Idempotency-Key": "equipment-key-1"})
	formID := formResult["id"].(string)
	call(base, "POST", "/v1/forms/equipment-request/submit", intern, submission, 200, map[string]string{"Idempotency-Key": "equipment-key-1"})
	call(base, "POST", "/v1/forms/equipment-request/submit", intern, strings.Replace(submission, "Laptop", "Monitor", 1), 409, map[string]string{"Idempotency-Key": "equipment-key-1"})
	call(base, "GET", "/v1/form-submissions/"+formID, intern, "", 200, nil)
	call(base, "GET", "/v1/form-submissions/"+formID, other, "", 404, nil)
	newForm := `{"id":"handoff","version":1,"title":"Handoff","kind":"general","schema":{"type":"object","properties":{"note":{"type":"string"}},"required":["note"]}}`
	call(base, "POST", "/v1/forms", intern, newForm, 403, nil)
	call(base, "POST", "/v1/forms", approver, newForm, 201, nil)
	call(base, "POST", "/v1/forms", approver, newForm, 409, nil)
	call(base, "POST", "/v1/forms", approver, `{}`, 400, nil)
	call(base, "POST", "/v1/forms", approver, `{"id":"external","version":1,"title":"Invalid","kind":"general","schema":{"properties":{"field":{"$ref":"https://example.test/schema"}}}}`, 400, nil)
	call(base, "POST", "/v1/forms", approver, `{"id":"invalid","version":1,"title":"Invalid","kind":"general","schema":{"type":7}}`, 400, nil)

	// A form submission must create exactly one pending access request, even
	// when the browser retries with a different idempotency key.
	if _, err := rights.DB.Exec(ctx, `INSERT INTO ta_resources(id,team_id,kind,name,capabilities,approver_id) VALUES('demo-docs','demo-team','document','Documents',ARRAY['read'],'approver')`); err != nil {
		t.Fatal(err)
	}
	call(base, "GET", "/v1/access-requests?limit=0", intern, "", 400, nil)
	call(base, "GET", "/v1/access-requests?offset=-1", intern, "", 400, nil)
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":"missing","capability":"read"}`, 404, nil)
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-docs","capability":"read"}`, 200, nil)
	call(base, "POST", "/v1/access-requests", intern, `{"resource_id":"demo-docs","capability":"read","reason":"Need docs","confirmed":false}`, 400, map[string]string{"Idempotency-Key": "docs-key-1"})
	formAccess := `{"values":{"resource_id":"demo-docs","capability":"read","reason":"Need docs"},"confirmed":true,"version":1}`
	access := call(base, "POST", "/v1/forms/access-request/submit", intern, formAccess, 201, map[string]string{"Idempotency-Key": "docs-form-key-1"})
	accessID := access["id"].(string)
	call(base, "POST", "/v1/forms/access-request/submit", intern, formAccess, 200, map[string]string{"Idempotency-Key": "docs-form-key-2"})
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-docs","capability":"read"}`, 200, nil)
	call(base, "GET", "/v1/access-requests?form_id=access-request&limit=1", intern, "", 200, nil)
	call(base, "POST", "/v1/access-requests", intern, `{"resource_id":"demo-docs","capability":"read","reason":"Changed reason","confirmed":true}`, 409, map[string]string{"Idempotency-Key": "docs-form-key-1"})
	call(base, "POST", "/v1/access-requests/"+accessID+"/decision", approver, `{"decision":"bad"}`, 400, nil)
	call(base, "POST", "/v1/access-requests/"+accessID+"/decision", approver, `{"decision":"reject","comment":"Please clarify"}`, 200, nil)
	check := call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-docs","capability":"read"}`, 200, nil)
	if check["state"] != "missing" {
		t.Fatalf("rejected request granted access: %v", check)
	}
	call(base, "POST", "/v1/access-requests", intern, `{"resource_id":"demo-calendar","capability":"read","reason":"Already granted","confirmed":true}`, 409, map[string]string{"Idempotency-Key": "granted-key-1"})

	// Storage failures must remain explicit and never be presented as a missing
	// permission or an empty catalogue.
	if _, err := rights.DB.Exec(ctx, `DROP TABLE ta_requests`); err != nil {
		t.Fatal(err)
	}
	call(base, "GET", "/v1/access-requests", intern, "", 503, nil)
	call(base, "GET", "/v1/access-requests/"+id, intern, "", 503, nil)
	call(base, "GET", "/v1/approvals", approver, "", 503, nil)
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-docs","capability":"read"}`, 503, nil)
	call(base, "POST", "/v1/access-requests/"+id+"/decision", approver, `{"decision":"approve"}`, 503, nil)
	call(base, "POST", "/v1/access-requests", intern, `{"resource_id":"demo-docs","capability":"read","reason":"Need docs","confirmed":true}`, 503, map[string]string{"Idempotency-Key": "outage-key-1"})
	if _, err := rights.DB.Exec(ctx, `DROP TABLE ta_resources CASCADE`); err != nil {
		t.Fatal(err)
	}
	call(base, "GET", "/v1/teams/demo-team/resources", intern, "", 503, nil)
	call(base, "POST", "/v1/access/check", intern, `{"resource_id":"demo-docs","capability":"read"}`, 503, nil)
	if _, err := rights.DB.Exec(ctx, `DROP TABLE form_submissions`); err != nil {
		t.Fatal(err)
	}
	call(base, "GET", "/v1/form-submissions/"+formID, intern, "", 503, nil)
	if _, err := rights.DB.Exec(ctx, `DROP TABLE form_definitions`); err != nil {
		t.Fatal(err)
	}
	call(base, "GET", "/v1/forms", intern, "", 503, nil)
	call(base, "GET", "/v1/forms/equipment-request", intern, "", 503, nil)
}
