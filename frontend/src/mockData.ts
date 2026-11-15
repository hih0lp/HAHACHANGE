export type MockSource = 'jira' | 'calendar' | 'company-calendar' | 'gitlab';
export type Snapshot<T> = { version: number; data: T };
export type JiraIssue = {
  key: string; summary: string; description?: string; status: string; status_category: string;
  due_date?: string; due_at?: string; start_date?: string;
  assignee_account_id?: string; assignee_name?: string;
};
export type JiraData = { project_key: string; issues: JiraIssue[] };
export type CalendarEntry = {
  id: string; summary: string; description?: string; status?: string;
  start: { dateTime: string; timeZone?: string }; end: { dateTime: string; timeZone?: string };
};
export type CalendarData = { calendar_id: string; summary: string; time_zone: string; events: CalendarEntry[] };

export type GitLabOverview = {
  id: number; name: string; path_with_namespace: string; default_branch: string; description?: string;
};
export type GitLabBranch = { name: string; protected: boolean; commit_id: string };
export type GitLabCommit = { id: string; title: string; message: string; author_name: string; created_at: string };
export type GitLabMergeRequest = {
  iid: number; title: string; description: string; state: string; draft: boolean;
  source_branch: string; target_branch: string; updated_at: string; reviewer: string; approved: boolean;
};
export type GitLabProject = GitLabOverview & {
  owner: string; branches: GitLabBranch[]; commits: GitLabCommit[]; merge_requests: GitLabMergeRequest[];
};
export type GitLabData = { projects: GitLabProject[] };
export type MockData = JiraData | CalendarData | GitLabData;
