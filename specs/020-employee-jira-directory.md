# SDD-020: Jira identity in the team directory

The team directory stores each employee's Jira account ID and profile URL. `GET /v1/me` returns the caller's Jira identity. `GET /v1/teams/{team_id}/members` returns Jira identities only for members of a team the caller belongs to; the same fields are available through `team_profile` and `team_members` MCP tools. Existing team membership authorization remains in force.

Seed identities use `demo-<user-id>` and links under `https://jira.example.invalid/`. They are explicit demonstration data and cannot fetch real Jira tasks. Operators may set `TEAM_JIRA_ACCOUNTS_JSON` to a JSON object mapping directory user IDs to real Jira account IDs, together with an HTTPS `JIRA_BASE_URL`. A configured real identity updates that user's directory record on startup. Jira API credentials remain in the Jira adapter's `JIRA_EMAIL` and `JIRA_API_TOKEN` configuration and are never returned by the directory or MCP.

The current deadline reminder is a separate single-user feature in the API. It requires live Jira and `REMINDER_INTERN_ACCOUNT_ID`, polls once per minute, and displays assigned unfinished tasks within `REMINDER_HOURS_BEFORE` hours of the due date. This directory change does not turn it into a per-employee scheduler.
