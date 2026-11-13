package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/teamaccess"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConfirmedAccessFormPersistsOnlyOnce(t *testing.T) {
	ctx := context.Background()
	teamDB, rightsDB := testsupport.Postgres(t), testsupport.Postgres(t)
	migrations := filepath.Join("..", "..", "migrations", "team_access")
	t.Setenv("TEAM_ACCESS_KEY_FILE", filepath.Join(t.TempDir(), "test-key.pem"))
	t.Setenv("TEAM_ACCESS_INTERN_PASSWORD", "temporary-test-password")
	t.Setenv("TEAM_ACCESS_INTERN2_PASSWORD", "temporary-test-password-two")
	t.Setenv("TEAM_ACCESS_APPROVER_PASSWORD", "temporary-test-password-three")
	t.Setenv("TEAM_ACCESS_ROLE", "team")
	t.Setenv("TEAM_ACCESS_MIGRATION", filepath.Join(migrations, "001_team.sql"))
	team, err := teamaccess.New(ctx, teamDB)
	if err != nil {
		t.Fatal(err)
	}
	directory := httptest.NewServer(team.Routes())
	defer directory.Close()

	t.Setenv("TEAM_ACCESS_ROLE", "rights")
	t.Setenv("TEAM_ACCESS_MIGRATION", filepath.Join(migrations, "001_rights.sql"))
	t.Setenv("TEAM_DIRECTORY_URL", directory.URL)
	t.Setenv("TEAM_ACCESS_SEED_JIRA_URL", "https://example.atlassian.net")
	rights, err := teamaccess.New(ctx, rightsDB)
	if err != nil {
		t.Fatal(err)
	}
	rightsHTTP := httptest.NewServer(rights.Routes())
	defer rightsHTTP.Close()
	t.Setenv("RIGHTS_SELF_URL", rightsHTTP.URL)

	api := scenario.Client{BaseURL: directory.URL, RightsURL: rightsHTTP.URL}
	login, err := api.Login(ctx, "intern", "temporary-test-password")
	if err != nil {
		t.Fatal(err)
	}
	token := login.AccessToken
	forms, err := api.Forms(ctx, token)
	if err != nil || len(forms) == 0 {
		t.Fatalf("forms = %+v, %v", forms, err)
	}
	form, err := api.Form(ctx, token, "access-request")
	if err != nil {
		t.Fatal(err)
	}
	resources, err := api.Resources(ctx, token, "demo-team")
	if err != nil || len(resources) != 1 {
		t.Fatalf("resources = %+v, %v", resources, err)
	}
	values := map[string]any{"resource_id": resources[0].ID, "capability": resources[0].Capabilities[0], "reason": "Проверка первой задачи"}
	valid, err := api.ValidateForm(ctx, token, form.ID, values)
	if err != nil || valid["valid"] != true {
		t.Fatalf("validation = %+v, %v", valid, err)
	}

	confirmed := httptest.NewServer(newRightsMCP(true))
	defer confirmed.Close()
	httpClient := &http.Client{Transport: bearerTransport{token: token}}
	client := mcp.NewClient(&mcp.Implementation{Name: "bobby-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: confirmed.URL, HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var requestID any
	for i := 0; i < 2; i++ {
		result, callErr := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "rights_form_submit",
			Arguments: map[string]any{"form_id": form.ID, "version": form.Version, "values": values, "idempotency_key": "confirmed-bobby-test"},
		})
		if callErr != nil || result.IsError {
			t.Fatalf("confirmation = %+v, %v", result, callErr)
		}
		submitted, ok := result.StructuredContent.(map[string]any)
		if !ok || submitted["id"] == nil || submitted["status"] != "pending" {
			t.Fatalf("confirmation result = %+v", result)
		}
		if i == 0 {
			requestID = submitted["id"]
		} else if requestID != submitted["id"] {
			t.Fatalf("retry returned a different ID: %v / %v", requestID, submitted["id"])
		}
	}
	var count int
	if err := rightsDB.QueryRow(ctx, `SELECT COUNT(*) FROM ta_requests`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("persisted requests = %d, %v", count, err)
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	next := request.Clone(request.Context())
	next.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(next)
}
