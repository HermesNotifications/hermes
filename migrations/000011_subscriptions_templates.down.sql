-- Recreate notification_groups
CREATE TABLE notification_groups (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    default_channels TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_notification_groups_slug ON notification_groups (slug);

-- Recreate notification_types
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

-- Recreate user_preferences
CREATE TABLE user_preferences (
    user_id TEXT NOT NULL REFERENCES users(id),
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    channels TEXT[],
    PRIMARY KEY (user_id, group_id)
);

-- Revert notifications column renames: drop new FK constraints first
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_template_id_fkey;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_category_id_fkey;

ALTER TABLE notifications RENAME COLUMN template_id TO type_id;
ALTER TABLE notifications RENAME COLUMN category_id TO group_id;

-- Re-add original FK constraints
ALTER TABLE notifications ADD CONSTRAINT notifications_type_id_fkey FOREIGN KEY (type_id) REFERENCES notification_types(id);
ALTER TABLE notifications ADD CONSTRAINT notifications_group_id_fkey FOREIGN KEY (group_id) REFERENCES notification_groups(id);

-- Drop new tables
DROP TABLE IF EXISTS user_subscriptions;
DROP TABLE IF EXISTS notification_templates;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS subscription_categories;
