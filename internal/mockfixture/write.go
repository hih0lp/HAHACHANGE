package mockfixture

import (
	"context"
	"crypto/subtle"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const limit = 2 << 20

type input struct {
	JSON string `json:"json" jsonschema:"Complete JSON fixture document"`
}

// Register keeps privileged tools separate from the assistant's read catalog.
func Register(mux *http.ServeMux, path, tool, token string, validate func([]byte) error) {
	if token == "" {
		return
	}
	save := func(data []byte) error {
		dir := filepath.Dir(path)
		f, err := os.CreateTemp(dir, ".fixture-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if _, err := f.Write(data); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(f.Name(), path)
	}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Values("Authorization")
			if len(auth) != 1 || subtle.ConstantTimeCompare([]byte(auth[0]), []byte("Bearer "+token)) != 1 {
				http.Error(w, "fixture authorization required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	mux.Handle("PUT /v1/fixture", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if err != nil {
			http.Error(w, "fixture body unreadable or too large", http.StatusBadRequest)
			return
		}
		if err := validate(data); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := save(data); err != nil {
			log.Printf("fixture save: %v", err)
			http.Error(w, "fixture write failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	server := mcp.NewServer(&mcp.Implementation{Name: tool, Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: tool, Description: "Replace the mock service JSON fixture."}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, map[string]bool, error) {
		if len(in.JSON) > limit {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "fixture too large"}}}, nil, nil
		}
		if err := validate([]byte(in.JSON)); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
		}
		if err := save([]byte(in.JSON)); err != nil {
			log.Printf("fixture save: %v", err)
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "fixture write failed"}}}, nil, nil
		}
		return nil, map[string]bool{"saved": true}, nil
	})
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux.Handle("/mcp/admin", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 6*limit)
		stream.ServeHTTP(w, r)
	})))
}
