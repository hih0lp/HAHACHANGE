# SDD-059: Load and save the Jira task description

The Jira task editor loads the current task card when opened. `GET /v1/mock-data/jira/issues/{key}` returns only that task's fields, including its saved description. A valid team JWT is required. The assigned intern and the mentor may read the card; another intern receives 404. The Bobby API forwards this addressed GET to the Jira mock. The full dataset endpoint remains mentor-only.

The browser fills the title, description and deadline from the current card. Saving sends only fields whose values differ from the loaded card. An empty description is a valid change and clears the saved description. The editor stays open and shows an error if loading or saving fails. Reopening the editor reads the saved value again.

Verification covers description PUT and subsequent read, the assignee boundary, proxy authentication, and browser interactions for changing and clearing a description.
