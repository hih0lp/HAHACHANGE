package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hackchange/avito-rag/internal/scenario"
)

func teamToolResult(ctx context.Context, api scenario.Client, token, name, rawArgs string) any {
	if token == "" {
		return map[string]string{"error": "В этом запросе нет авторизации team-access."}
	}
	var args struct {
		TeamID string `json:"team_id"`
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
	default:
		return map[string]string{"error": fmt.Sprintf("Неизвестный инструмент %q.", name)}
	}
}
