ALTER TABLE jwt_signing_keys ALTER COLUMN organization_id_claim SET DEFAULT 'tenant_id';
UPDATE jwt_signing_keys SET organization_id_claim = 'tenant_id' WHERE organization_id_claim = 'organization_id';
ALTER TABLE jwt_signing_keys RENAME COLUMN organization_id_claim TO tenant_id_claim;

ALTER TABLE notifications RENAME CONSTRAINT notifications_organization_id_fkey TO notifications_tenant_id_fkey;
ALTER TABLE notifications RENAME COLUMN organization_id TO tenant_id;

ALTER INDEX idx_users_organization_external RENAME TO idx_users_tenant_external;
ALTER TABLE users RENAME CONSTRAINT users_organization_id_fkey TO users_tenant_id_fkey;
ALTER TABLE users RENAME COLUMN organization_id TO tenant_id;

ALTER TABLE organizations RENAME TO tenants;
