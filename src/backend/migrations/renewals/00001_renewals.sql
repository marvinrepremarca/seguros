-- Renovaciones (Flujo 6): un caso por póliza expedida, alertas y retención.
-- +goose Up
CREATE SCHEMA renewals;
GRANT USAGE ON SCHEMA renewals TO seguros_app;

CREATE TABLE renewals.cases (
    id              uuid PRIMARY KEY,
    tenant_id       uuid NOT NULL,
    policy_id       uuid NOT NULL,
    policy_number   text NOT NULL,
    insurer_id      uuid NOT NULL,
    holder_party_id uuid NOT NULL,
    effective_to    date NOT NULL,
    status          text NOT NULL CHECK (status IN ('open', 'in_progress', 'renewed', 'lost', 'closed')),
    lost_reason     text CHECK (char_length(lost_reason) <= 500),
    renewed_by      uuid,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    updated_by      uuid,
    version         integer NOT NULL CHECK (version > 0),
    CONSTRAINT cases_policy_key UNIQUE (tenant_id, policy_id)
);
CREATE INDEX cases_due_idx ON renewals.cases (tenant_id, effective_to, id);

CREATE TABLE renewals.alerts (
    tenant_id uuid NOT NULL,
    policy_id uuid NOT NULL,
    days      integer NOT NULL,
    sent_at   timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, policy_id, days)
);

-- Días de alerta antes del fin de vigencia (DECISIONS T2).
CREATE TABLE renewals.settings (
    tenant_id  uuid PRIMARY KEY,
    alert_days integer[] NOT NULL,
    updated_at timestamptz NOT NULL,
    updated_by uuid
);

ALTER TABLE renewals.cases ENABLE ROW LEVEL SECURITY;
ALTER TABLE renewals.cases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON renewals.cases USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE renewals.alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE renewals.alerts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON renewals.alerts USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE renewals.settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE renewals.settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON renewals.settings USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
-- Solo lectura entre tenants para la función del barrido.
CREATE POLICY owner_scan ON renewals.cases FOR SELECT TO CURRENT_USER USING (true);

GRANT SELECT, INSERT, UPDATE ON renewals.cases TO seguros_app;
GRANT SELECT, INSERT ON renewals.alerts TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON renewals.settings TO seguros_app;

-- +goose StatementBegin
CREATE FUNCTION renewals.tenants_with_open_cases(p_until date)
RETURNS TABLE (tenant_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT DISTINCT c.tenant_id FROM renewals.cases c
    WHERE c.status IN ('open', 'in_progress') AND c.effective_to <= p_until
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION renewals.tenants_with_open_cases(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION renewals.tenants_with_open_cases(date) TO seguros_worker;

-- +goose Down
DROP FUNCTION renewals.tenants_with_open_cases(date);
DROP TABLE renewals.settings;
DROP TABLE renewals.alerts;
DROP TABLE renewals.cases;
DROP SCHEMA renewals;
