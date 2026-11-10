package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hackchange/avito-rag/internal/scenario"
)

// A verified snapshot is sent to the worker. The JWT stays in the API process.
func teamAccessContext(ctx context.Context, api scenario.Client, token string) (json.RawMessage, error) {
	profile, err := api.Profile(ctx, token)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"source": "team-access", "profile": profile}
	if profile.CurrentTeamID == "" {
		return json.Marshal(result)
	}
	resources, err := api.Resources(ctx, token, profile.CurrentTeamID)
	if err != nil {
		return nil, err
	}
	if len(resources) > 40 {
		return nil, errors.New("team catalog exceeds context limit")
	}
	checks := make([]scenario.Check, 0)
	for _, resource := range resources {
		if len(resource.Capabilities) > 15 {
			return nil, errors.New("resource exceeds capability limit")
		}
		for _, capability := range resource.Capabilities {
			check, err := api.Check(ctx, token, resource.ID, capability)
			if err != nil {
				return nil, err
			}
			if check.ResourceID != resource.ID || check.Capability != capability {
				return nil, errors.New("access service returned mismatched result")
			}
			checks = append(checks, check)
		}
	}
	result["resources"] = resources
	result["checks"] = checks
	return json.Marshal(result)
}
