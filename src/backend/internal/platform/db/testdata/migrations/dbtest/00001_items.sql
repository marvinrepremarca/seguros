-- Esquema de prueba de platform/db: una tabla de negocio con RLS según rules/database.md.
-- +goose Up
CREATE SCHEMA dbtest;
GRANT USAGE ON SCHEMA dbtest TO seguros_app;

CREATE TABLE dbtest.items (
    id           uuid PRIMARY KEY,
    tenant_id    uuid NOT NULL,
    name         text NOT NULL,
    amount       numeric(19, 4) NOT NULL,
    currency     char(3) NOT NULL,
    effective_on date NOT NULL
);
CREATE INDEX items_tenant_idx ON dbtest.items (tenant_id);

ALTER TABLE dbtest.items ENABLE ROW LEVEL SECURITY;
ALTER TABLE dbtest.items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dbtest.items
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON dbtest.items TO seguros_app;

-- +goose Down
DROP TABLE dbtest.items;
DROP SCHEMA dbtest;
