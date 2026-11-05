-- Días antes del primer vencimiento en que se acepta un pago, por agencia (DECISIONS N22).
-- +goose Up
CREATE TABLE billing.payment_settings (
    tenant_id  uuid PRIMARY KEY,
    lead_days  integer NOT NULL CHECK (lead_days BETWEEN 0 AND 3650),
    updated_at timestamptz NOT NULL,
    updated_by uuid
);
ALTER TABLE billing.payment_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing.payment_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing.payment_settings
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON billing.payment_settings TO seguros_app;

-- +goose Down
DROP TABLE billing.payment_settings;
