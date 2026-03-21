CREATE TABLE tenants (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    default_locale TEXT NOT NULL DEFAULT 'en',
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
