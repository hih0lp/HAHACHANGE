package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type rightsEmptyArg struct{}
type rightsTeamArg struct {
	TeamID string `json:"team_id" jsonschema:"team ID from the user's team profile"`
}
type rightsCheckArg struct {
	ResourceID string `json:"resource_id" jsonschema:"resource ID from rights_resources"`
	Capability string `json:"capability" jsonschema:"capability supported by that resource"`
}
type rightsFormArg struct {
	FormID string `json:"form_id" jsonschema:"form ID from rights_forms_list"`
}
type rightsValidateArg struct {
	FormID string         `json:"form_id" jsonschema:"form ID from rights_forms_list"`
	Values map[string]any `json:"values" jsonschema:"field values matching the published JSON Schema"`
}
type rightsSubmitArg struct {
	FormID         string         `json:"form_id"`
	Version        int            `json:"version"`
	Values         map[string]any `json:"values"`
	IdempotencyKey string         `json:"idempotency_key"`
}

var errRightsToken = errors.New("access-rights authorization is required")

// The rights service owns this MCP endpoint. Tools use the caller's JWT and
// the same HTTP contract as other clients, so there is one authorization path.
func newRightsMCP(allowSubmit bool) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == r.Header.Get("Authorization") {
			token = ""
		}
		selfURL := os.Getenv("RIGHTS_SELF_URL")
		if selfURL == "" {
			selfURL = "http://127.0.0.1:8080"
		}
		api := scenario.Client{BaseURL: selfURL}
		server := mcp.NewServer(&mcp.Implementation{Name: "hackchange-access-rights", Version: "1.0.0"}, nil)
		mcp.AddTool(server, &mcp.Tool{Name: "rights_resources", Description: "List resources and capabilities visible to the current user in a team."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg rightsTeamArg) (*mcp.CallToolResult, any, error) {
			if token == "" {
				return nil, nil, errRightsToken
			}
			v, err := api.Resources(ctx, token, arg.TeamID)
			return nil, v, err
		})
		mcp.AddTool(server, &mcp.Tool{Name: "rights_access_check", Description: "Check the current user's access to a resource capability."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg rightsCheckArg) (*mcp.CallToolResult, any, error) {
			if token == "" {
				return nil, nil, errRightsToken
			}
			v, err := api.Check(ctx, token, arg.ResourceID, arg.Capability)
			return nil, v, err
		})
		mcp.AddTool(server, &mcp.Tool{Name: "rights_forms_list", Description: "Discover current forms for requests handled by the rights service."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ rightsEmptyArg) (*mcp.CallToolResult, any, error) {
			if token == "" {
				return nil, nil, errRightsToken
			}
			v, err := api.Forms(ctx, token)
			if err != nil {
				return nil, nil, err
			}
			forms := make([]map[string]any, 0, len(v))
			for _, f := range v {
				forms = append(forms, map[string]any{"id": f.ID, "version": f.Version, "title": f.Title, "description": f.Description, "kind": f.Kind, "form_url": "/forms/" + url.PathEscape(f.ID)})
			}
			return nil, map[string]any{"forms": forms}, nil
		})
		mcp.AddTool(server, &mcp.Tool{Name: "rights_form_get", Description: "Read a form's fields, validation schema and labels before offering it in chat."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg rightsFormArg) (*mcp.CallToolResult, any, error) {
			if token == "" {
				return nil, nil, errRightsToken
			}
			v, err := api.Form(ctx, token, arg.FormID)
			return nil, v, err
		})
		mcp.AddTool(server, &mcp.Tool{Name: "rights_form_validate", Description: "Validate collected field values without submitting the form. Submission requires a separate user confirmation in chat."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg rightsValidateArg) (*mcp.CallToolResult, any, error) {
			if token == "" {
				return nil, nil, errRightsToken
			}
			v, err := api.ValidateForm(ctx, token, arg.FormID, arg.Values)
			return nil, v, err
		})
		if allowSubmit {
			mcp.AddTool(server, &mcp.Tool{Name: "rights_form_submit", Description: "Submit a form after the user confirmed it in the chat interface."}, func(ctx context.Context, _ *mcp.CallToolRequest, arg rightsSubmitArg) (*mcp.CallToolResult, any, error) {
				if token == "" {
					return nil, nil, errRightsToken
				}
				v, err := api.SubmitForm(ctx, token, arg.IdempotencyKey, arg.FormID, arg.Version, arg.Values)
				return nil, v, err
			})
		}
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
