CREATE TABLE IF NOT EXISTS chat_groups (
    id text PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_group_members (
    group_id text REFERENCES chat_groups(id),
    user_id text,
    display_name text NOT NULL,
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    fixture_key text UNIQUE,
    group_id text NOT NULL,
    author_id text NOT NULL,
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 1000),
    sent_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    embedding real[] NOT NULL,
    FOREIGN KEY (group_id, author_id) REFERENCES chat_group_members(group_id, user_id)
);

CREATE INDEX IF NOT EXISTS chat_messages_group_time_idx ON chat_messages (group_id, sent_at, id);
CREATE INDEX IF NOT EXISTS chat_group_members_user_idx ON chat_group_members (user_id, group_id);
