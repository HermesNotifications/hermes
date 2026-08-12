-- Reverses 000020.
--
-- DESTRUCTIVE AND IRREVERSIBLE: dropping the column discards every notification's metadata,
-- including the level and toast flags senders supplied. There is no way to reconstruct it -- the
-- originating send requests are not retained anywhere.
ALTER TABLE notifications DROP COLUMN IF EXISTS metadata;
