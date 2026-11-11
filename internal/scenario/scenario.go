package scenario

import (
	"context"
	"errors"
	"time"
)

type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Profile struct {
	ID             string `json:"id"`
	JiraAccountID  string `json:"jira_account_id,omitempty"`
	JiraProfileURL string `json:"jira_profile_url,omitempty"`
	Teams          []Team `json:"teams"`
	CurrentTeamID  string `json:"current_team_id"`
}
type Resource struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	ExternalRef  string   `json:"external_ref"`
	URL          string   `json:"url"`
	Capabilities []string `json:"capabilities"`
}
type Check struct {
	State      string    `json:"state"`
	ResourceID string    `json:"resource_id"`
	Capability string    `json:"capability"`
	RequestID  string    `json:"request_id,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}
type Request struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	ResourceID string `json:"resource_id"`
	Capability string `json:"capability"`
}
type AccessInput struct {
	ResourceID string            `json:"resource_id"`
	Capability string            `json:"capability"`
	Reason     string            `json:"reason"`
	FormValues map[string]string `json:"form_values"`
	Confirmed  bool              `json:"confirmed"`
}
type Widget struct {
	Visible    bool      `json:"visible"`
	Type       string    `json:"type"`
	Action     string    `json:"action,omitempty"`
	Resource   *Resource `json:"resource,omitempty"`
	Capability string    `json:"capability,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
}
type State struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"scenario_id"`
	OwnerID       string    `json:"-"`
	Revision      int       `json:"revision"`
	Goal          string    `json:"goal"`
	TeamID        string    `json:"team_id"`
	Status        string    `json:"status"`
	Message       string    `json:"message"`
	Checks        []Check   `json:"checks"`
	Widget        Widget    `json:"widget"`
	UpdatedAt     time.Time `json:"updated_at"`
}
type Access interface {
	Profile(context.Context, string) (Profile, error)
	Resources(context.Context, string, string) ([]Resource, error)
	Check(context.Context, string, string, string) (Check, error)
	CreateRequest(context.Context, string, string, AccessInput) (Request, error)
}

// Advance checks only the nearest prerequisite. A team catalog is not a list
// of requirements for every task. Later steps must be grounded in the issue.
func Advance(ctx context.Context, api Access, token string, state State, profile Profile) (State, error) {
	if profile.ID != state.OwnerID {
		return state, errors.New("scenario owner mismatch")
	}
	state.Widget = Widget{}
	if state.TeamID == "" {
		state.TeamID = profile.CurrentTeamID
		if state.TeamID == "" && len(profile.Teams) == 1 {
			state.TeamID = profile.Teams[0].ID
		}
	}
	member := false
	for _, team := range profile.Teams {
		if team.ID == state.TeamID {
			member = true
		}
	}
	if !member {
		state.Status = "needs_input"
		state.Message = "Нужно определить текущую команду. Уточните её у наставника или в профиле."
		return state, nil
	}
	resources, err := api.Resources(ctx, token, state.TeamID)
	if err != nil {
		return state, err
	}
	var jira *Resource
	for i := range resources {
		if resources[i].Kind == "jira_project" {
			if jira != nil {
				state.Status = "needs_input"
				state.Message = "У команды несколько Jira-проектов. Нужно уточнить у наставника проект первой задачи."
				return state, nil
			}
			jira = &resources[i]
		}
	}
	if jira == nil {
		state.Status = "needs_input"
		state.Message = "В каталоге команды не указан Jira-проект. Уточните его у наставника."
		return state, nil
	}
	check, err := api.Check(ctx, token, jira.ID, "browse_issues")
	if err != nil {
		return state, err
	}
	if check.ResourceID != jira.ID || check.Capability != "browse_issues" {
		return state, errors.New("access service returned mismatched resource")
	}
	replaced := false
	for i, c := range state.Checks {
		if c.ResourceID == check.ResourceID && c.Capability == check.Capability {
			state.Checks[i] = check
			replaced = true
		}
	}
	if !replaced {
		state.Checks = append(state.Checks, check)
	}
	switch check.State {
	case "missing":
		state.Status = "needs_input"
		state.Message = "Для получения первой задачи нужен доступ к " + jira.Name + ". Заполните обоснование и подтвердите заявку."
		state.Widget = Widget{Visible: true, Type: "access_request_form", Action: "request_access", Resource: jira, Capability: "browse_issues"}
	case "pending":
		state.Status = "waiting_access"
		state.Message = "Заявка рассматривается. После решения повторно проверим доступ. Пока можно обсудить с наставником первую задачу."
		state.Widget = Widget{Visible: true, Type: "approval_status", Action: "refresh", Resource: jira, Capability: "browse_issues", RequestID: check.RequestID}
	case "granted":
		state.Status = "ready_for_task"
		state.Message = "Доступ к Jira-проекту подтверждён. Следующий шаг — выбрать первую задачу с наставником."
		state.Widget = Widget{Visible: true, Type: "task_selection", Action: "select_task", Resource: jira}
	case "unknown":
		state.Status = "check_unavailable"
		state.Message = "Сервис не подтвердил результат проверки. Повторим проверку; заявка пока не отправлена."
		state.Widget = Widget{Visible: true, Type: "retry", Action: "refresh"}
	default:
		return state, errors.New("unknown access state")
	}
	return state, nil
}
