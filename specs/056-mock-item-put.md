# SDD-056: Addressed mock item updates

The three database-backed mock services expose single-item PUT routes. A valid team JWT is required; any authenticated role may edit these demo records. The service checks the token with the team directory, locks the current dataset row, changes only the requested item, validates the resulting complete fixture, saves it atomically, and increments the dataset version. REST and MCP readers use the same updated dataset. Missing records return 404, invalid fields or dates return 400, and invalid tokens return 403.

| Service | Route | Body |
| --- | --- | --- |
| Jira | `PUT /v1/mock-data/jira/issues/{key}` | Any of `summary`, `description`, `due_date`, `due_at`. Date uses `YYYY-MM-DD`; timestamp uses RFC3339. `null` clears an optional date or description. |
| Calendar | `PUT /v1/mock-data/calendar/events/{id}` | `start` and/or `end` as RFC3339 strings or objects with `dateTime` and optional `timeZone`. |
| GitLab | `PUT /v1/mock-data/gitlab/projects/{project}/branches/{name...}` | `{"name":"new/branch"}`. Project is numeric ID. Renaming updates the default branch and merge request references. |

These routes edit the database-backed mocks. The existing versioned full-dataset `PUT /v1/mock-data/{service}` remains available to the mentor. Local file-backed development mode retains its existing `PUT /v1/fixture` editor. Revisions to fixture files after database seeding do not overwrite saved database edits.

The Bobby API proxies each addressed PUT through the same `/v1/mock-data/...` path to the corresponding mock service, forwarding the caller's JWT. The mock services remain on the private Compose network.

The addressed PUT response contains only `{ "version": <number>, "saved": true }`. It does not return the full dataset, which may contain records assigned to other users.

Tests cover an intern JWT, invalid JWT, missing item, invalid payload, persistence, dates, and GitLab branch references.
