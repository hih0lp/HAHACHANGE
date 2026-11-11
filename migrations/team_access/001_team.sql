CREATE TABLE IF NOT EXISTS ta_users (
  id text PRIMARY KEY, username text NOT NULL UNIQUE, display_name text NOT NULL,
  password_hash text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE ta_users ADD COLUMN IF NOT EXISTS jira_account_id text;
ALTER TABLE ta_users ADD COLUMN IF NOT EXISTS jira_profile_url text;
CREATE TABLE IF NOT EXISTS ta_teams (
  id text PRIMARY KEY, name text NOT NULL, mentor_id text REFERENCES ta_users(id), lead_id text REFERENCES ta_users(id)
);
CREATE TABLE IF NOT EXISTS ta_memberships (
  user_id text NOT NULL REFERENCES ta_users(id), team_id text NOT NULL REFERENCES ta_teams(id),
  role text NOT NULL, PRIMARY KEY(user_id,team_id)
);
