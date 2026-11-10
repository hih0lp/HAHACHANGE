CREATE TABLE IF NOT EXISTS ai_jobs (
    id uuid PRIMARY KEY,
    owner_id text NOT NULL,
    scenario text NOT NULL,
    query text NOT NULL,
    status text NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    answer text,
    sources jsonb NOT NULL DEFAULT '[]'::jsonb,
    error text,
    attempt integer NOT NULL DEFAULT 0,
    lease_token uuid,
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE INDEX IF NOT EXISTS ai_jobs_owner_created_idx ON ai_jobs (owner_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_jobs_recovery_idx ON ai_jobs (lease_until) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS ai_job_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES ai_jobs(id),
    event_type text NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (job_id, event_type)
);

CREATE INDEX IF NOT EXISTS ai_job_outbox_pending_idx ON ai_job_outbox (id) WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS ai_actions (
    id uuid PRIMARY KEY,
    owner_id text NOT NULL,
    request_id uuid NOT NULL REFERENCES ai_jobs(id),
    idempotency_key text NOT NULL,
    kind text NOT NULL,
    resource text NOT NULL,
    reason text NOT NULL,
    mode text NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'running', 'uncertain', 'completed', 'failed')),
    external_key text,
    external_url text,
    external_status text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, idempotency_key)
);
