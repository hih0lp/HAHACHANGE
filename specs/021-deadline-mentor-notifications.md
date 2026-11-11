# SDD-021: Deadline notices in both demo chats

The API checks the configured live Jira project once per minute. A Jira due date means the end of that day in `REMINDER_TIMEZONE` (Europe/Moscow by default). Only unfinished tasks assigned to the demo intern's Jira account ID produce notifications. A real `jira_account_id` from the team directory takes precedence over `REMINDER_INTERN_ACCOUNT_ID`; demo IDs cannot query real Jira. The team directory profile identifies the intern's current-team mentor.

At most one `intern_48h` notice is stored for the intern per issue and due date when the deadline enters the last 48 hours. At most one `mentor_24h` notice is stored for the mentor when it enters the last 24 hours. The unique database key is recipient, kind, issue key and due timestamp. A changed due date gets a new notice. Notices are persisted even if the browser is closed and are read from `GET /v1/deadline-notifications` for the selected demo role.

The demo role switch chooses the preconfigured `intern` or `approver` account for team-access and separates chat history by role within the browser session. This is a testing convenience, not production authentication. The API does not post Jira comments or send external messages. Both notices render in the chat with background `#FFF3D6`, left border `#D97706`, and text `#442500`, with distinct headings. Regular answers remain white.

After the GitLab/Calendar branch merge, the chat no longer subscribes to the older `/v1/reminders` snapshot/WebSocket. That API remains available for compatibility, but only these persisted notices render as Jira deadline cards. This prevents a task from appearing twice in the same chat.
