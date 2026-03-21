CREATE TABLE user_preferences (
    user_id TEXT NOT NULL REFERENCES users(id),
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    channels TEXT[],
    PRIMARY KEY (user_id, group_id)
);
