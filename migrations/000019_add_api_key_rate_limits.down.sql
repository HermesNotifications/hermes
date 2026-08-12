ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_rate_limit_per_second_positive,
    DROP CONSTRAINT IF EXISTS api_keys_rate_limit_burst_positive;

ALTER TABLE api_keys
    DROP COLUMN IF EXISTS rate_limit_per_second,
    DROP COLUMN IF EXISTS rate_limit_burst;
