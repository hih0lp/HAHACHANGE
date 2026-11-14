package mockfixture

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.json")
	mux := http.NewServeMux()
	Register(mux, path, "save_fixture", "test-token", func([]byte) error { return nil })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer test-token")
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp/admin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Name != "save_fixture" {
		t.Fatalf("tools: %v, %v", list, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "save_fixture", Arguments: map[string]string{"json": "{}"}})
	if err != nil || result.IsError {
		t.Fatalf("call: %v, %v", result, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{}" {
		t.Fatalf("file %q: %v", data, err)
	}
}

func TestPutFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, path, "save_fixture", "test-token", func(data []byte) error {
		if string(data) != "{}" {
			return errors.New("invalid fixture")
		}
		return nil
	})
	for _, c := range []struct {
		token, body, saved string
		status             int
	}{
		{"", "{}", "old", 401},
		{"test-token", "invalid", "old", 400},
		{"test-token", "{}", "{}", 204},
	} {
		r := httptest.NewRequest(http.MethodPut, "/v1/fixture", strings.NewReader(c.body))
		r.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		data, err := os.ReadFile(path)
		if err != nil || w.Code != c.status || string(data) != c.saved {
			t.Fatalf("status %d, file %q, err %v", w.Code, data, err)
		}
	}
}
