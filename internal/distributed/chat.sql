CREATE TABLE IF NOT EXISTS ai_chat_threads (
    id uuid PRIMARY KEY,
    owner_id text NOT NULL,
    title text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE ai_jobs ADD COLUMN IF NOT EXISTS thread_id uuid REFERENCES ai_chat_threads(id);
CREATE INDEX IF NOT EXISTS ai_chat_threads_owner_updated_idx ON ai_chat_threads (owner_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS ai_jobs_owner_thread_created_idx ON ai_jobs (owner_id, thread_id, created_at DESC, id DESC);
