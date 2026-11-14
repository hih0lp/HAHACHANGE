ALTER TABLE ai_deadline_notifications DROP CONSTRAINT IF EXISTS ai_deadline_notifications_kind_check;
ALTER TABLE ai_deadline_notifications ADD CONSTRAINT ai_deadline_notifications_kind_check
    CHECK (kind IN ('intern_48h', 'mentor_24h', 'intern_deadline', 'mentor_deadline'));
