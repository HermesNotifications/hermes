UPDATE notifications SET category_id = 'sct_default_general' WHERE category_id IS NULL;
ALTER TABLE notifications ALTER COLUMN category_id SET NOT NULL;
