ALTER TABLE tenants RENAME TO organizations;

ALTER TABLE users RENAME COLUMN tenant_id TO organization_id;
ALTER TABLE users RENAME CONSTRAINT users_tenant_id_fkey TO users_organization_id_fkey;
ALTER INDEX idx_users_tenant_external RENAME TO idx_users_organization_external;

ALTER TABLE notifications RENAME COLUMN tenant_id TO organization_id;
ALTER TABLE notifications RENAME CONSTRAINT notifications_tenant_id_fkey TO notifications_organization_id_fkey;

ALTER TABLE jwt_signing_keys RENAME COLUMN tenant_id_claim TO organization_id_claim;
UPDATE jwt_signing_keys SET organization_id_claim = 'organization_id' WHERE organization_id_claim = 'tenant_id';
ALTER TABLE jwt_signing_keys ALTER COLUMN organization_id_claim SET DEFAULT 'organization_id';
