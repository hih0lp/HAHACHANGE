CREATE TABLE IF NOT EXISTS ta_users (
  id text PRIMARY KEY, username text NOT NULL UNIQUE, display_name text NOT NULL,
  password_hash text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ta_teams (
  id text PRIMARY KEY, name text NOT NULL, mentor_id text REFERENCES ta_users(id), lead_id text REFERENCES ta_users(id)
);
CREATE TABLE IF NOT EXISTS ta_memberships (
  user_id text NOT NULL REFERENCES ta_users(id), team_id text NOT NULL REFERENCES ta_teams(id),
  role text NOT NULL, PRIMARY KEY(user_id,team_id)
);
