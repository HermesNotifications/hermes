CREATE TABLE notification_events (
    id TEXT PRIMARY KEY,
    notification_id TEXT NOT NULL REFERENCES notifications(id),
    channel TEXT NOT NULL,
    event TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_notification_events_notification ON notification_events (notification_id, created_at);
