package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRightsMCPCatalogSeparatesConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		allowSubmit bool
		wantSubmit  bool
	}{
		{"agent", false, false},
		{"confirmed_chat", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(newRightsMCP(tc.allowSubmit))
			defer server.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "rights-test", Version: "1"}, nil)
			session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			catalog, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, tool := range catalog.Tools {
				found[tool.Name] = true
			}
			for _, name := range []string{"rights_resources", "rights_access_check", "rights_forms_list", "rights_form_get", "rights_form_validate"} {
				if !found[name] {
					t.Errorf("missing %s", name)
				}
			}
			if found["rights_form_submit"] != tc.wantSubmit {
				t.Errorf("submit tool visible = %v", found["rights_form_submit"])
			}
		})
	}
}
