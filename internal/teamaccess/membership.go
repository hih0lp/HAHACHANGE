package teamaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// teamRole checks the directory on every rights operation. A resource ID or
// model-supplied team ID is never sufficient to authorize a request.
func (s *Service) teamRole(r *http.Request, team string) (string, bool, error) {
	if s.role != "rights" {
		var role string
		err := s.DB.QueryRow(r.Context(), `SELECT role FROM ta_memberships WHERE user_id=$1 AND team_id=$2`, user(r), team).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return role, true, nil
	}
	if s.teamURL == "" {
		return "", false, errors.New("TEAM_DIRECTORY_URL is required")
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(s.teamURL, "/")+"/v1/teams/"+url.PathEscape(team)+"/members", nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("team directory HTTP %d", resp.StatusCode)
	}
	var result struct {
		Members []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, err
	}
	for _, member := range result.Members {
		if member.ID == user(r) {
			return member.Role, true, nil
		}
	}
	return "", false, nil
}
