-- Integraciones con aseguradoras: bitácora de llamadas a los conectores (sin datos del riesgo).
-- +goose Up
CREATE SCHEMA integrations;
GRANT USAGE ON SCHEMA integrations TO seguros_app;

CREATE TABLE integrations.calls (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    connector  text NOT NULL CHECK (char_length(connector) BETWEEN 2 AND 60),
    operation  text NOT NULL CHECK (char_length(operation) BETWEEN 2 AND 60),
    outcome    text NOT NULL CHECK (outcome IN ('success', 'failure')),
    created_at timestamptz NOT NULL,
    created_by uuid
);
CREATE INDEX calls_tenant_keyset_idx ON integrations.calls (tenant_id, id DESC);

ALTER TABLE integrations.calls ENABLE ROW LEVEL SECURITY;
ALTER TABLE integrations.calls FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integrations.calls USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT ON integrations.calls TO seguros_app;

-- +goose Down
DROP TABLE integrations.calls;
DROP SCHEMA integrations;
