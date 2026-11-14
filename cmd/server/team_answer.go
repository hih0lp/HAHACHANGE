package main

import (
	"encoding/json"
	"errors"
	"strings"
)

// formatTeamMembers uses only the directory's verified names and relationship
// flags. Internal roles, user IDs and Jira identities are not user-facing facts.
func formatTeamMembers(result any) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	var response struct {
		Members []struct {
			DisplayName string `json:"display_name"`
			IsMentor    *bool  `json:"is_mentor"`
			IsLead      *bool  `json:"is_lead"`
		} `json:"members"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Members == nil {
		return "", errors.New("invalid team members")
	}
	if len(response.Members) == 0 {
		return "Участники текущей команды не найдены.", nil
	}
	lines := []string{"Участники текущей команды:"}
	mentors, leads := []string{}, []string{}
	for _, member := range response.Members {
		name := strings.Join(strings.Fields(member.DisplayName), " ")
		if name == "" || member.IsMentor == nil || member.IsLead == nil {
			return "", errors.New("incomplete team member")
		}
		lines = append(lines, "- "+name)
		if *member.IsMentor {
			mentors = append(mentors, name)
		}
		if *member.IsLead {
			leads = append(leads, name)
		}
	}
	if len(mentors) > 0 {
		lines = append(lines, "Наставник: "+strings.Join(mentors, ", "))
	}
	if len(leads) > 0 {
		lines = append(lines, "Руководитель: "+strings.Join(leads, ", "))
	}
	return strings.Join(lines, "\n"), nil
}
