-- Scope an API key to the organization it may act for.
--
-- Until now nothing tied a key to an organization. /v1/send read the organization
-- from the REQUEST BODY, so any key holding notifications:send could address any
-- organization — one customer could deliver notifications into another customer's
-- users' inboxes given only an organization ID.
--
-- The column is NULLABLE on purpose. Making it NOT NULL here would require guessing
-- which organization every existing key belongs to, and there is no signal in the
-- data to guess from: a wrong guess either breaks a working integration or
-- mis-attributes its sends, both silently. Instead:
--
--   * a key WITH an organization is enforced — it may only act for that organization;
--   * a key WITHOUT one keeps today's behaviour, and is reported by the
--     hermes.auth.unscoped_key_uses metric so operators can find and migrate them.
--
-- Tightening to NOT NULL is a follow-up migration, once that metric reads zero.
-- See docs/adr/0011-api-keys-are-scoped-to-an-organization.md.
ALTER TABLE api_keys
    ADD COLUMN organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE;

-- Supports "which keys belong to this organization", which the admin listing needs
-- once keys are scoped, and the ON DELETE CASCADE above.
CREATE INDEX idx_api_keys_organization_id ON api_keys (organization_id);
