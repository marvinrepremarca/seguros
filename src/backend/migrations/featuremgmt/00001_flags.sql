-- Estado persistido de los feature flags (ARCHITECTURE.md §7). Las definiciones viven en
-- internal/platform/flags/registry.yaml; aquí solo los overrides por tenant y los switches
-- globales. Apagar un flag nunca borra datos de negocio (rules/feature-flags.md §Datos).
-- +goose Up
CREATE SCHEMA featuremgmt;
GRANT USAGE ON SCHEMA featuremgmt TO seguros_app;

-- Override de un flag para un tenant (add-on comercial o apagado explícito). Sin fila = plan/default.
CREATE TABLE featuremgmt.tenant_overrides (
    tenant_id  uuid        NOT NULL,
    flag_key   text        NOT NULL CHECK (flag_key ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*){1,2}$' AND char_length(flag_key) <= 120),
    enabled    boolean     NOT NULL,
    reason     text        NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 500),
    created_at timestamptz NOT NULL,
    created_by uuid,
    updated_at timestamptz NOT NULL,
    updated_by uuid,
    version    integer     NOT NULL CHECK (version > 0),
    PRIMARY KEY (tenant_id, flag_key)
);

ALTER TABLE featuremgmt.tenant_overrides ENABLE ROW LEVEL SECURITY;
ALTER TABLE featuremgmt.tenant_overrides FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON featuremgmt.tenant_overrides
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON featuremgmt.tenant_overrides TO seguros_app;

-- Kill switches y flags ops globales (no son datos de un tenant: sin tenant_id ni RLS). Solo los
-- cambia el super-administrador con cmd/platformctl; la API no expone escritura.
CREATE TABLE featuremgmt.global_switches (
    flag_key   text        PRIMARY KEY CHECK (flag_key ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*){1,2}$' AND char_length(flag_key) <= 120),
    enabled    boolean     NOT NULL,
    reason     text        NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 500),
    updated_at timestamptz NOT NULL
);

GRANT SELECT, INSERT, UPDATE, DELETE ON featuremgmt.global_switches TO seguros_app;

-- +goose Down
DROP TABLE featuremgmt.global_switches;
DROP TABLE featuremgmt.tenant_overrides;
DROP SCHEMA featuremgmt;
