package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

func (a answerer) accessResources(ctx context.Context, token string) ([]resourceChoice, error) {
	if a.teamMCPURL == "" || a.rightsMCPURL == "" || token == "" {
		return nil, errors.New("team or rights service is unavailable")
	}
	profile, err := mcpTeamTool(ctx, a.teamMCPURL, "", token, "team_profile", map[string]any{})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var identity struct {
		CurrentTeamID string `json:"current_team_id"`
	}
	if json.Unmarshal(data, &identity) != nil || identity.CurrentTeamID == "" {
		return nil, errors.New("current team is unavailable")
	}
	result, err := mcpTeamTool(ctx, a.rightsMCPURL, "", token, "rights_resources", map[string]any{"team_id": identity.CurrentTeamID})
	if err != nil {
		return nil, err
	}
	data, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var resources []resourceChoice
	if json.Unmarshal(data, &resources) != nil || resources == nil {
		return nil, errors.New("resource catalogue is unavailable")
	}
	return resources, nil
}

func (a answerer) plannedAccessAnswer(ctx context.Context, token string, plan calendarPlan, resources []resourceChoice) string {
	var selected *resourceChoice
	for i := range resources {
		if resources[i].ID == plan.ResourceID && plan.ResourceID != "" {
			selected = &resources[i]
			break
		}
	}
	if selected == nil {
		return "Не удалось однозначно определить ресурс для проверки доступа. Уточните его название."
	}
	capabilities := selected.Capabilities
	if plan.Capability != "" {
		capabilities = nil
		for _, capability := range selected.Capabilities {
			if capability == plan.Capability {
				capabilities = []string{capability}
				break
			}
		}
		if len(capabilities) == 0 {
			return "Выбранное право не найдено в каталоге ресурса «" + selected.Name + "»."
		}
	}
	if len(capabilities) == 0 {
		return "В каталоге ресурса «" + selected.Name + "» нет прав для проверки."
	}
	lines := []string{}
	for _, capability := range capabilities {
		result, err := mcpTeamTool(ctx, a.rightsMCPURL, "", token, "rights_access_check", map[string]any{"resource_id": selected.ID, "capability": capability})
		state := "состояние не удалось проверить"
		if err == nil {
			data, marshalErr := json.Marshal(result)
			var current struct {
				State string `json:"state"`
			}
			if marshalErr == nil && json.Unmarshal(data, &current) == nil {
				switch current.State {
				case "granted":
					state = "право выдано в учебном прототипе"
				case "pending":
					state = "заявка ожидает решения в учебном прототипе"
				case "missing":
					state = "в прототипе право не выдано"
				}
			}
		}
		if len(capabilities) == 1 {
			return "Доступ к «" + selected.Name + "»: " + state + "."
		}
		lines = append(lines, "- "+capability+": "+state)
	}
	return "Доступ к «" + selected.Name + "»:\n" + strings.Join(lines, "\n")
}
