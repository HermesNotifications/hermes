ALTER TABLE notification_events
  ADD CONSTRAINT notification_events_notification_id_fkey
  FOREIGN KEY (notification_id) REFERENCES notifications(id);
