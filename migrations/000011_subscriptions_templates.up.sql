-- Create subscription_categories table
CREATE TABLE subscription_categories (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    default_channels TEXT[] NOT NULL DEFAULT '{}',
    default_state TEXT NOT NULL DEFAULT 'on',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_subscription_categories_slug ON subscription_categories (slug);

-- Create subscriptions table
CREATE TABLE subscriptions (
    id TEXT PRIMARY KEY,
    category_id TEXT NOT NULL REFERENCES subscription_categories(id),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_subscriptions_category_slug ON subscriptions (category_id, slug);

-- Seed default categories
INSERT INTO subscription_categories (id, slug, name, default_channels, default_state, sort_order) VALUES
    ('sct_default_account',   'account',   'Account',   '{email,inbox}', 'required', 0),
    ('sct_default_general',   'general',   'General',   '{email,inbox}', 'on',       1),
    ('sct_default_marketing', 'marketing', 'Marketing', '{email}',       'off',      2);

-- Seed default subscriptions
INSERT INTO subscriptions (id, category_id, slug, name, sort_order) VALUES
    ('sub_default_account',   'sct_default_account',   'account',   'Account',   0),
    ('sub_default_general',   'sct_default_general',   'general',   'General',   0),
    ('sub_default_marketing', 'sct_default_marketing', 'marketing', 'Marketing', 0);

-- Create notification_templates table
CREATE TABLE notification_templates (
    id TEXT PRIMARY KEY,
    subscription_id TEXT REFERENCES subscriptions(id),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    default_channels TEXT[] NOT NULL DEFAULT '{}',
    email_subject TEXT,
    email_body TEXT,
    sms_body TEXT,
    inbox_title TEXT,
    inbox_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_notification_templates_slug ON notification_templates (slug);

-- Migrate existing notification_types → notification_templates
-- Assign all to sub_default_general; copy default_channels from the type's group
INSERT INTO notification_templates (id, subscription_id, slug, name, default_channels, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at)
SELECT
    nt.id,
    'sub_default_general',
    nt.slug,
    nt.name,
    ng.default_channels,
    nt.email_subject,
    nt.email_body,
    nt.sms_body,
    nt.inbox_title,
    nt.inbox_body,
    nt.created_at
FROM notification_types nt
JOIN notification_groups ng ON ng.id = nt.group_id;

-- Create user_subscriptions table
CREATE TABLE user_subscriptions (
    user_id TEXT NOT NULL REFERENCES users(id),
    subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
    opted_in BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, subscription_id)
);

-- Alter notifications table: drop old FK constraints, rename columns, add new FKs

-- Drop FK constraints on type_id and group_id
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_id_fkey;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_group_id_fkey;

-- Rename type_id → template_id and group_id → category_id
ALTER TABLE notifications RENAME COLUMN type_id TO template_id;
ALTER TABLE notifications RENAME COLUMN group_id TO category_id;

-- Update category_id to sct_default_general for all existing rows
UPDATE notifications SET category_id = 'sct_default_general';

-- Add new FK constraints
ALTER TABLE notifications ADD CONSTRAINT notifications_template_id_fkey FOREIGN KEY (template_id) REFERENCES notification_templates(id);
ALTER TABLE notifications ADD CONSTRAINT notifications_category_id_fkey FOREIGN KEY (category_id) REFERENCES subscription_categories(id);

-- Drop old tables (in FK-safe order)
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS notification_types;
DROP TABLE IF EXISTS notification_groups;
