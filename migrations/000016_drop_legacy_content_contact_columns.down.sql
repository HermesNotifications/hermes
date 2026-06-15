ALTER TABLE users
  ADD COLUMN email TEXT,
  ADD COLUMN phone TEXT;

ALTER TABLE notification_templates
  ADD COLUMN email_subject TEXT,
  ADD COLUMN email_body TEXT,
  ADD COLUMN sms_body TEXT,
  ADD COLUMN inbox_title TEXT,
  ADD COLUMN inbox_body TEXT;
