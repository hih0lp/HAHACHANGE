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
CREATE TABLE IF NOT EXISTS ta_resources (
  id text PRIMARY KEY, team_id text NOT NULL REFERENCES ta_teams(id), kind text NOT NULL,
  name text NOT NULL, external_ref text NOT NULL DEFAULT '', url text NOT NULL DEFAULT '',
  capabilities text[] NOT NULL, approver_id text NOT NULL REFERENCES ta_users(id)
);
CREATE TABLE IF NOT EXISTS ta_grants (
  user_id text NOT NULL REFERENCES ta_users(id), resource_id text NOT NULL REFERENCES ta_resources(id),
  capability text NOT NULL, granted_by text NOT NULL REFERENCES ta_users(id),
  granted_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,resource_id,capability)
);
CREATE TABLE IF NOT EXISTS ta_requests (
  id text PRIMARY KEY, user_id text NOT NULL REFERENCES ta_users(id),
  resource_id text NOT NULL REFERENCES ta_resources(id), capability text NOT NULL,
  approver_id text NOT NULL REFERENCES ta_users(id), reason text NOT NULL,
  form_values jsonb NOT NULL DEFAULT '{}'::jsonb, status text NOT NULL CHECK(status IN ('pending','approved','rejected')),
  decision_reason text NOT NULL DEFAULT '', idempotency_key text NOT NULL,
  input_hash text NOT NULL, revision bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,idempotency_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS ta_one_pending ON ta_requests(user_id,resource_id,capability) WHERE status='pending';
CREATE INDEX IF NOT EXISTS ta_requests_user_created ON ta_requests(user_id,created_at DESC);
CREATE INDEX IF NOT EXISTS ta_requests_approver ON ta_requests(approver_id,created_at DESC) WHERE status='pending';
CREATE TABLE IF NOT EXISTS ta_audit (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, actor_id text NOT NULL, subject_id text NOT NULL,
  request_id text, action text NOT NULL, occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ta_outbox (
  event_id text PRIMARY KEY, request_id text NOT NULL, event_type text NOT NULL,
  payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), published_at timestamptz
);
CREATE INDEX IF NOT EXISTS ta_outbox_pending ON ta_outbox(created_at) WHERE published_at IS NULL;
