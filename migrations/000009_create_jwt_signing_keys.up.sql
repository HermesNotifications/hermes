CREATE TABLE jwt_signing_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    algorithm TEXT NOT NULL DEFAULT 'HS256',
    secret TEXT NOT NULL,
    user_id_claim TEXT NOT NULL DEFAULT 'sub',
    tenant_id_claim TEXT NOT NULL DEFAULT 'tenant_id',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
