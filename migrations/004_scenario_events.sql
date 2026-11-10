CREATE TABLE IF NOT EXISTS ai_scenario_events (
  event_id text PRIMARY KEY,
  received_at timestamptz NOT NULL DEFAULT now()
);
