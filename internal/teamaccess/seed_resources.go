package teamaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type seedResource struct {
	ID           string   `json:"id"`
	TeamID       string   `json:"team_id"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	ExternalRef  string   `json:"external_ref"`
	URL          string   `json:"url"`
	Capabilities []string `json:"capabilities"`
	ApproverID   string   `json:"approver_id"`
}

func loadSeedResources(path string) ([]seedResource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fixture struct {
		Resources []seedResource `json:"resources"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		return nil, err
	}
	if fixture.Resources == nil {
		return nil, errors.New("resources array is required")
	}
	seen := map[string]bool{}
	for _, resource := range fixture.Resources {
		if resource.ID == "" || resource.TeamID == "" || resource.Kind == "" || resource.Name == "" || resource.ExternalRef == "" || (resource.URL != "" && !strings.HasPrefix(resource.URL, "https://")) || resource.ApproverID == "" || len(resource.Capabilities) == 0 || seen[resource.ID] {
			return nil, fmt.Errorf("invalid or duplicate resource %q", resource.ID)
		}
		seen[resource.ID] = true
		for _, capability := range resource.Capabilities {
			if capability == "" {
				return nil, fmt.Errorf("empty capability in resource %q", resource.ID)
			}
		}
	}
	return fixture.Resources, nil
}
