-- CONCURRENTLY, and one statement only, for the same reasons as the up migration.
--
-- Reversing this restores the primary-key backward scan on the unread-count watermark and the
-- sequential scan on the user-deletion FK check. Neither is an error, so nothing will fail --
-- they just get slow in proportion to the size of the notifications table.
DROP INDEX CONCURRENTLY IF EXISTS idx_notifications_user_id;
