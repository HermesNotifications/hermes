CREATE TABLE notifications (
    id TEXT PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    type_id TEXT REFERENCES notification_types(id),
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    action_url TEXT,
    action_label TEXT,
    idempotency_key TEXT,
    channels TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_notifications_inbox ON notifications (user_id, created_at DESC) WHERE archived_at IS NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX idx_notifications_idempotency ON notifications (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
