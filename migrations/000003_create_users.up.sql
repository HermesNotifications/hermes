CREATE TABLE users (
    id TEXT PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    external_id TEXT NOT NULL,
    email TEXT,
    phone TEXT,
    locale TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_users_tenant_external ON users (tenant_id, external_id);
