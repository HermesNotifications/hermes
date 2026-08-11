-- CONCURRENTLY, and one statement only, for the same reasons as the up migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_notifications_unread;
