-- Qué datos debe conservar una renovación, por agencia (DECISIONS N25).
-- +goose Up
CREATE TABLE policies.renewal_settings (
    tenant_id        uuid PRIMARY KEY,
    keep_holder      boolean NOT NULL,
    keep_line        boolean NOT NULL,
    updated_at       timestamptz NOT NULL,
    updated_by       uuid
);
ALTER TABLE policies.renewal_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.renewal_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.renewal_settings
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON policies.renewal_settings TO seguros_app;

-- +goose Down
DROP TABLE policies.renewal_settings;
