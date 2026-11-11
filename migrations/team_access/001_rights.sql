CREATE TABLE IF NOT EXISTS ta_resources (
  id text PRIMARY KEY, team_id text NOT NULL, kind text NOT NULL,
  name text NOT NULL, external_ref text NOT NULL DEFAULT '', url text NOT NULL DEFAULT '',
  capabilities text[] NOT NULL, approver_id text NOT NULL
);
CREATE TABLE IF NOT EXISTS ta_grants (
  user_id text NOT NULL, resource_id text NOT NULL REFERENCES ta_resources(id),
  capability text NOT NULL, granted_by text NOT NULL,
  granted_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,resource_id,capability)
);
CREATE TABLE IF NOT EXISTS ta_requests (
  id text PRIMARY KEY, user_id text NOT NULL,
  resource_id text NOT NULL REFERENCES ta_resources(id), capability text NOT NULL,
  approver_id text NOT NULL, reason text NOT NULL,
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

CREATE TABLE IF NOT EXISTS form_definitions (
  id text NOT NULL, version integer NOT NULL CHECK(version > 0), title text NOT NULL,
  description text NOT NULL DEFAULT '', kind text NOT NULL CHECK(kind IN ('access','general')),
  schema jsonb NOT NULL, ui_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  published boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(id,version)
);
CREATE TABLE IF NOT EXISTS form_submissions (
  id text PRIMARY KEY, form_id text NOT NULL, form_version integer NOT NULL,
  user_id text NOT NULL, values jsonb NOT NULL, status text NOT NULL DEFAULT 'submitted',
  idempotency_key text NOT NULL, input_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(form_id,form_version) REFERENCES form_definitions(id,version),
  UNIQUE(user_id,idempotency_key)
);
ALTER TABLE ta_requests ADD COLUMN IF NOT EXISTS form_id text;
ALTER TABLE ta_requests ADD COLUMN IF NOT EXISTS form_version integer;

INSERT INTO form_definitions(id,version,title,description,kind,schema,ui_schema,published) VALUES
('access-request',1,'Заявка на доступ','Запросить право на доступный ресурс','access',
 '{"type":"object","properties":{"resource_id":{"type":"string","minLength":1},"capability":{"type":"string","minLength":1},"reason":{"type":"string","minLength":2,"maxLength":1000}},"required":["resource_id","capability","reason"],"additionalProperties":false}',
 '{"order":["resource_id","capability","reason"],"labels":{"resource_id":"Ресурс","capability":"Право","reason":"Зачем нужен доступ"}}',true),
('equipment-request',1,'Запрос оборудования','Учебное обращение без автоматической выдачи прав','general',
 '{"type":"object","properties":{"item":{"type":"string","minLength":2},"reason":{"type":"string","minLength":2}},"required":["item","reason"],"additionalProperties":false}',
 '{"order":["item","reason"],"labels":{"item":"Оборудование","reason":"Зачем нужно"}}',true)
ON CONFLICT(id,version) DO NOTHING;
