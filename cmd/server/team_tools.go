package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func teamToolResult(ctx context.Context, api scenario.Client, token, name, rawArgs string) any {
	if token == "" {
		return map[string]string{"error": "В этом запросе нет авторизации team-access."}
	}
	var args struct {
		TeamID     string `json:"team_id"`
		FormID     string `json:"form_id"`
		ResourceID string `json:"resource_id"`
		Capability string `json:"capability"`
	}
	if rawArgs != "" && json.Unmarshal([]byte(rawArgs), &args) != nil {
		return map[string]string{"error": "Некорректные аргументы инструмента."}
	}
	currentTeam := func() (string, error) {
		profile, err := api.Profile(ctx, token)
		if err != nil {
			return "", err
		}
		return profile.CurrentTeamID, nil
	}
	switch name {
	case "team_profile":
		result, err := api.Profile(ctx, token)
		if err != nil {
			return map[string]string{"error": err.Error()}
		}
		return result
	case "team_members":
		teamID := strings.TrimSpace(args.TeamID)
		if teamID == "" {
			var err error
			teamID, err = currentTeam()
			if err != nil || teamID == "" {
				return map[string]string{"error": "Не удалось определить текущую команду."}
			}
		}
		result, err := api.Members(ctx, token, teamID)
		if err != nil {
			return map[string]string{"error": err.Error()}
		}
		return map[string]any{"team_id": teamID, "members": result}
	case "team_resources":
		teamID := strings.TrimSpace(args.TeamID)
		if teamID == "" {
			var err error
			teamID, err = currentTeam()
			if err != nil || teamID == "" {
				return map[string]string{"error": "Не удалось определить текущую команду."}
			}
		}
		result, err := api.Resources(ctx, token, teamID)
		if err != nil {
			return map[string]string{"error": err.Error()}
		}
		return map[string]any{"team_id": teamID, "resources": result}
	case "access_check":
		if strings.TrimSpace(args.ResourceID) == "" || strings.TrimSpace(args.Capability) == "" {
			return map[string]string{"error": "resource_id и capability обязательны."}
		}
		result, err := api.Check(ctx, token, args.ResourceID, args.Capability)
		if err != nil {
			return map[string]string{"error": err.Error()}
		}
		return result
	case "forms_list":
		result, err := api.Forms(ctx, token)
		if err != nil {
			return map[string]string{"error": err.Error()}
		}
		forms := make([]map[string]any, 0, len(result))
		for _, f := range result {
			forms = append(forms, map[string]any{"form": f, "form_url": "/forms/" + url.PathEscape(f.ID)})
		}
		return map[string]any{"forms": forms}
	case "form_get":
		if args.FormID == "" {
			return map[string]string{"error": "form_id is required"}
		}
		result, err := api.Form(ctx, token, args.FormID)
		if err != nil {
			return map[string]string{"error": err.Error()}
		}
		return map[string]any{"form": result, "form_url": "/forms/" + url.PathEscape(result.ID)}
	default:
		return map[string]string{"error": fmt.Sprintf("Неизвестный инструмент %q.", name)}
	}
}
