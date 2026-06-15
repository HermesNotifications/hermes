-- Normalized per-channel template content and per-address-key user contact points.
-- Phase 2a: create + backfill. Old fixed columns are KEPT here and dropped in 000016 (phase 2e).

CREATE TABLE template_channel_content (
    template_id  TEXT  NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    channel_slug TEXT  NOT NULL,
    content      JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (template_id, channel_slug)
);

CREATE TABLE user_contact_points (
    user_id     TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address_key TEXT    NOT NULL,
    address     TEXT    NOT NULL,
    verified    BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (user_id, address_key)
);

-- Backfill template content from the fixed columns. jsonb_strip_nulls drops absent fields
-- so a template with only a subject doesn't store a null body.
INSERT INTO template_channel_content (template_id, channel_slug, content)
SELECT id, 'email', jsonb_strip_nulls(jsonb_build_object('subject', email_subject, 'body', email_body))
FROM notification_templates
WHERE email_subject IS NOT NULL OR email_body IS NOT NULL;

INSERT INTO template_channel_content (template_id, channel_slug, content)
SELECT id, 'sms', jsonb_build_object('body', sms_body)
FROM notification_templates
WHERE sms_body IS NOT NULL;

INSERT INTO template_channel_content (template_id, channel_slug, content)
SELECT id, 'inbox', jsonb_strip_nulls(jsonb_build_object('title', inbox_title, 'body', inbox_body))
FROM notification_templates
WHERE inbox_title IS NOT NULL OR inbox_body IS NOT NULL;

-- Backfill contact points from users.email / users.phone.
INSERT INTO user_contact_points (user_id, address_key, address, verified)
SELECT id, 'email', email, FALSE FROM users WHERE email IS NOT NULL AND email <> '';

INSERT INTO user_contact_points (user_id, address_key, address, verified)
SELECT id, 'phone', phone, FALSE FROM users WHERE phone IS NOT NULL AND phone <> '';
