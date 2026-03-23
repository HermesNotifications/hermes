ALTER TABLE api_keys ADD COLUMN permissions TEXT[] NOT NULL DEFAULT '{}';
