package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/teamaccess"
	"github.com/hackchange/avito-rag/internal/testsupport"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport string

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

func TestGroupChat(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Setenv("TEAM_ACCESS_ROLE", "team")
	t.Setenv("TEAM_ACCESS_MIGRATION", "../../migrations/team_access/001_team.sql")
	t.Setenv("TEAM_ACCESS_KEY_FILE", filepath.Join(t.TempDir(), "key.pem"))
	t.Setenv("TEAM_ACCESS_INTERN_PASSWORD", "chat-demo-intern")
	t.Setenv("TEAM_ACCESS_INTERN2_PASSWORD", "chat-demo-intern2")
	t.Setenv("TEAM_ACCESS_APPROVER_PASSWORD", "chat-demo-approver")
	directory, err := teamaccess.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	teamHTTP := httptest.NewServer(directory.Routes())
	defer teamHTTP.Close()
	team := scenario.Client{BaseURL: teamHTTP.URL}

	tei := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Inputs   []string `json:"inputs"`
			Truncate bool     `json:"truncate"`
		}
		if r.Method != "POST" || r.URL.Path != "/embed" || json.NewDecoder(r.Body).Decode(&payload) != nil || !payload.Truncate {
			http.Error(w, "bad TEI request", 400)
			return
		}
		var vectors [][]float32
		for _, input := range payload.Inputs {
			vector := make([]float32, 384)
			switch {
			case strings.Contains(input, "обновление выдачи"), strings.Contains(input, "новому ранжированию"):
				vector[0] = 1
			case strings.HasPrefix(input, "query:"):
				vector[2] = 1
			default:
				vector[1] = 1
			}
			vectors = append(vectors, vector)
		}
		json.NewEncoder(w).Encode(vectors)
	}))
	defer tei.Close()
	s := chatService{store{db}, embedder{tei.URL, tei.Client()}, team}
	now := time.Date(2026, 11, 14, 17, 0, 0, 0, demoZone)
	for _, day := range []time.Time{now, now.AddDate(0, 0, 1)} {
		if err := s.store.seed(ctx, s.embed, day); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM chat_messages").Scan(&count); err != nil || count != 10 {
		t.Fatalf("seed count = %d, err = %v", count, err)
	}
	server := httptest.NewServer(s.handler())
	defer server.Close()

	login := func(user string) string {
		t.Helper()
		result, err := team.Login(ctx, user, "chat-demo-"+user)
		if err != nil {
			t.Fatal(err)
		}
		return result.AccessToken
	}
	connect := func(token string) (*mcp.ClientSession, error) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		return client.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport(token)},
		}, nil)
	}
	for _, token := range []string{"", "fake", login("intern") + "tampered"} {
		if session, err := connect(token); err == nil {
			session.Close()
			t.Fatal("invalid token accepted")
		}
	}
	intern, err := connect(login("intern"))
	if err != nil {
		t.Fatal(err)
	}
	defer intern.Close()
	intern2, err := connect(login("intern2"))
	if err != nil {
		t.Fatal(err)
	}
	defer intern2.Close()
	tools, err := intern.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "search_group_messages" {
		t.Fatalf("tools = %+v, err = %v", tools, err)
	}
	search := func(session *mcp.ClientSession, query, group string) []message {
		t.Helper()
		args := map[string]any{"query": query, "from": "2026-11-13T00:00:00+03:00", "to": "2026-11-14T00:00:00+03:00"}
		if group != "" {
			args["group_id"] = group
		}
		got, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_group_messages", Arguments: args})
		if err != nil || got.IsError {
			t.Fatalf("search: %+v, err = %v", got, err)
		}
		payload, err := json.Marshal(got.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var result searchResult
		if err := json.Unmarshal(payload, &result); err != nil || result.Messages == nil {
			t.Fatalf("invalid structured result %s: %v", payload, err)
		}
		return result.Messages
	}

	got := search(intern, "обновление выдачи", "product")
	if len(got) != 3 || !strings.Contains(got[0].Text, "новому ранжированию") || !got[2].IsContext || !strings.Contains(got[2].Text, "Согласовано") {
		t.Fatalf("semantic match and context: %+v", got)
	}
	if got := search(intern, "SEARCH-42", "search"); len(got) != 0 {
		t.Fatalf("private messages leaked: %+v", got)
	}
	if got := search(intern2, "SEARCH-42", "search"); len(got) != 2 || !strings.Contains(got[0].Text, "SEARCH-42") {
		t.Fatalf("authorized exact match: %+v", got)
	}
	if got := search(intern, "5% трафика", "product"); len(got) != 0 {
		t.Fatalf("message outside requested period: %+v", got)
	}

	post := func(author, text string) (int, string) {
		t.Helper()
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		form := url.Values{"author": {author}, "text": {text}, "sent_at": {"2026-11-13T19:00"}}
		response, err := client.PostForm(server.URL+"/messages", form)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode, response.Header.Get("Location")
	}
	if code, _ := post("search:intern", "ignored"); code != 400 {
		t.Fatalf("non-member author accepted with status %d", code)
	}
	code, location := post("product:intern", "Предлагаю включить эксперимент после DEMO-123.")
	if code != 303 || !strings.HasPrefix(location, "/?added=") {
		t.Fatalf("post was not confirmed: %d %q", code, location)
	}
	if got := search(intern2, "DEMO-123", "product"); len(got) != 1 || got[0].ID != strings.TrimPrefix(location, "/?added=") {
		t.Fatalf("posted message not returned: %+v", got)
	}
}
