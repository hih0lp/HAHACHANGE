package main

// Team membership evidence does not need profile links to answer who belongs
// to the team. The seeded demo may contain placeholder or container-only URLs.
func withoutTeamProfileLinks(value any) any {
	switch v := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(v))
		for key, item := range v {
			if key != "jira_profile_url" {
				clean[key] = withoutTeamProfileLinks(item)
			}
		}
		return clean
	case []any:
		clean := make([]any, len(v))
		for i, item := range v {
			clean[i] = withoutTeamProfileLinks(item)
		}
		return clean
	default:
		return value
	}
}
