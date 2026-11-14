# SDD-032: pre-merge scenario check and GitLab shutdown

This specification supersedes the GitLab connectivity requirements in SDD-030 and SDD-031. At the user's request, Bobby no longer connects to GitLab. `compose.distributed.yml` sets `GITLAB_MCP_URL` and `GITLAB_REMINDER_PROJECT` to empty strings for API and worker, overriding any values in a private `.env`. The compatibility `compose.active-mcp.yml` also keeps them empty so existing deployment commands cannot reconnect GitLab. The isolated `compose.mock.yml` no longer launches a GitLab MCP container. The test project must remove its former GitLab container as an orphan on the next Compose update. The independent demo GitLab stack is outside this project's scope and is not stopped or modified.

Jira and Calendar remain connected only to their editable mock MCPs. Team directory, rights, access and equipment forms, Jira reminders, calendar reminders, RAG and chat continue to operate. The GitLab review reminder route is absent when its MCP URL is empty. Personalized recommendations may use team, documents and Jira; they must report GitLab as unavailable instead of inventing repository evidence. No fixed Jira keys, project names or question phrases are introduced for model routing.

The planner also emits `needs_gitlab` when a question requires GitLab evidence. With GitLab disconnected, Bobby gives an explicit unavailable answer for that intent. GitLab is optional for personalized recommendations: its absence does not prevent using verified team, Jira and document evidence.

The pre-merge chat found that the model correctly listed the intern's Jira issue but added unsupported advice about missing details and more pages. When the planner semantically selects a standalone personal-task list, Bobby now formats only the `jira_list_assigned_issues` payload: project, issue key, summary, status, optional due date and verified URL. It mentions truncation only when the MCP returns `truncated=true`. For a composite request, the same MCP payload remains available to the answering model as evidence. The formatting is independent of fixture values and does not inspect user phrasing.

Acceptance before a future server deployment: full Go tests and coverage pass; Compose resolves Jira/Calendar to mocks and GitLab MCP to empty; the isolated test project has no GitLab MCP container; specific and personal Jira questions, calendar events and busy time, team membership, both form links, and Jira/Calendar reminders work. A GitLab question must be answered as disconnected, without a project claim. The user requested publication of the current snapshot without further fixes, so the repeat server checks remain pending after publication. The production project still requires an operator with access to its private `.env` to redeploy; this task must not read that file.

Initial pre-merge run on 14 November 2026: all eight chat jobs completed and six read-only API routes succeeded. The specific Jira card, two calendar questions, team membership and both form links were correct. The personal Jira answer added unsupported tool/page advice, and the GitLab answer invented links and a tool name; these findings motivated the direct personal-list formatter and user-requested GitLab shutdown above. Local `go test -count=1 -cover ./...` then passed with 43.4% statement coverage for `cmd/server` and 26.4% for each mock. Repeat server checks after deploying this change are required before merge.

## Launch the isolated test stack

Use the dedicated server worktree `D:\HackChange\workspace\avito-rag-mock-services`. Create its untracked `.env` from `.env.example` with independent test passwords, session key, model URL, `JIRA_PROJECT_KEY`, `CALENDAR_DEMO_SUBJECT` and the intern Jira account mapping described in SDD-029. Do not copy the working stack's `.env`. From PowerShell in this worktree:

```powershell
$env:BOBBY_LOCAL_PORT='18001'
$env:BOBBY_REMOTE_BIND='127.0.0.2'
$env:BOBBY_REMOTE_PORT='18001'
$env:BOBBY_TEAM_PORT='18082'
$env:BOBBY_RIGHTS_PORT='18083'
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml config --quiet
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml up -d --build --remove-orphans
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml ps
```

Open `http://127.0.0.1:18001/` on the server. The `--remove-orphans` option removes the former GitLab MCP container from the **test** Compose project; it does not touch the separate demo or production projects. The API/worker configuration explicitly empties `GITLAB_MCP_URL` and `GITLAB_REMINDER_PROJECT`, even if old values remain in the private test `.env`. Edit `fixtures/mock/jira.json` or `fixtures/mock/calendar.json` to change responses on the next request, without restarting the mocks. Use `docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml logs --tail 50 api worker` for startup errors. Do not use `down -v` unless intentionally deleting test chat and account data.

Current handoff on 14 November 2026: the GitLab-disabled code passed local Go tests and was prepared for publication to GitHub `main` at the user's request, without further functional fixes. It has not been redeployed or rechecked on the server; these are explicit follow-up checks for an operator with access to the test or working deployment.
