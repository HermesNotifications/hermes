ALTER TABLE notification_templates
  DROP COLUMN email_subject,
  DROP COLUMN email_body,
  DROP COLUMN sms_body,
  DROP COLUMN inbox_title,
  DROP COLUMN inbox_body;

ALTER TABLE users
  DROP COLUMN email,
  DROP COLUMN phone;
