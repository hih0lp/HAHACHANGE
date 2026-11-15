# SDD-057: Edit individual mock records in the web app

On the **Мои задачи** page, each visible Jira task and mentor-calendar event has an **Изменить** button. Each visible GitLab project has a **Переименовать ветку** button. These buttons are available to all signed-in demo roles. Each opens a focused modal with labeled fields and an explicit save action. A successful save refreshes the visible lists; failures keep the modal open with an error.

The Jira modal can change the task title, replace its description, and change its due date or Moscow due time. It sends only fields changed in the form. The calendar modal changes start and end times in Moscow. The GitLab modal obtains the project's visible branch names from `GET /v1/tasks/gitlab/{project}/branches`, which calls the `gitlab_branches` MCP tool with the current JWT, then renames the selected branch through the addressed PUT route. The branch list is subject to the existing GitLab visibility checks.

The item modals use the addressed PUT endpoints from SDD-056 and never load a full mock dataset. The full snapshot editor remains available to the mentor for broader fixture management. The browser does not treat the selected demo role as authorization: each server operation validates the JWT.

Verification: TypeScript check and frontend build, focused backend route test, and browser smoke test on the test stand for each modal and save action.
