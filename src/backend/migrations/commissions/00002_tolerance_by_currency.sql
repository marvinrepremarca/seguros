-- Tolerancia de conciliación por moneda (DECISIONS T4, N26). El módulo no ha salido a ningún entorno:
-- la tabla anterior no tiene datos que migrar.
-- +goose Up
DROP TABLE commissions.settings;
CREATE TABLE commissions.tolerances (
    tenant_id  uuid NOT NULL,
    currency   char(3) NOT NULL,
    tolerance  numeric(19,4) NOT NULL CHECK (tolerance >= 0),
    updated_at timestamptz NOT NULL,
    updated_by uuid,
    PRIMARY KEY (tenant_id, currency)
);
ALTER TABLE commissions.tolerances ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.tolerances FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.tolerances
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON commissions.tolerances TO seguros_app;

-- +goose Down
DROP TABLE commissions.tolerances;
CREATE TABLE commissions.settings (
    tenant_id  uuid PRIMARY KEY,
    tolerance  numeric(19,4) NOT NULL CHECK (tolerance >= 0),
    updated_at timestamptz NOT NULL,
    updated_by uuid
);
ALTER TABLE commissions.settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.settings USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON commissions.settings TO seguros_app;
