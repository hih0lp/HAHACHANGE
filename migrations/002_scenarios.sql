CREATE TABLE IF NOT EXISTS ai_scenarios (
 id uuid PRIMARY KEY,
 owner_id text NOT NULL,
 revision integer NOT NULL CHECK(revision > 0),
 state jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ai_scenarios_owner_idx ON ai_scenarios(owner_id, updated_at DESC);
