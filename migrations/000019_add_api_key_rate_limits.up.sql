-- Per-credential rate limits.
--
-- NULL means "use the service default", which is the same sentinel
-- middleware.ResolveLimit already applies to a zero override. That keeps every
-- existing key on exactly the behaviour it has today without a backfill, and it
-- keeps "unset" distinguishable from "deliberately set to something small".
ALTER TABLE api_keys
    ADD COLUMN rate_limit_per_second INTEGER,
    ADD COLUMN rate_limit_burst      INTEGER;

-- A zero or negative limit would be indistinguishable from "unset" once it
-- reaches ResolveLimit, so reject it at the edge rather than let an operator
-- believe they had disabled a key by setting its limit to 0.
ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_rate_limit_per_second_positive
        CHECK (rate_limit_per_second IS NULL OR rate_limit_per_second > 0),
    ADD CONSTRAINT api_keys_rate_limit_burst_positive
        CHECK (rate_limit_burst IS NULL OR rate_limit_burst > 0);
