CREATE TABLE notification_types (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    email_subject TEXT,
    email_body TEXT,
    sms_body TEXT,
    inbox_title TEXT,
    inbox_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_notification_types_slug ON notification_types (slug);
