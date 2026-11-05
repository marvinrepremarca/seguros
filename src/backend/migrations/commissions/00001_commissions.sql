-- Comisiones (Flujo 9): causación por póliza, liquidaciones de la aseguradora, conciliación y comisión
-- interna. Las filas causadas son append-only salvo la marca de conciliación.
-- +goose Up
CREATE SCHEMA commissions;
GRANT USAGE ON SCHEMA commissions TO seguros_app;

CREATE TABLE commissions.policy_terms (
    tenant_id     uuid NOT NULL,
    policy_id     uuid NOT NULL,
    policy_number text NOT NULL,
    insurer_id    uuid NOT NULL,
    line_id       uuid NOT NULL,
    product_id    uuid,
    currency      char(3) NOT NULL,
    net_premium   numeric(19,4) NOT NULL,
    taxes         numeric(19,4) NOT NULL,
    PRIMARY KEY (tenant_id, policy_id)
);
CREATE INDEX policy_terms_number_idx ON commissions.policy_terms (tenant_id, insurer_id, policy_number);

CREATE TABLE commissions.policy_agents (
    id           uuid PRIMARY KEY,
    tenant_id    uuid NOT NULL,
    policy_id    uuid NOT NULL,
    agent_id     uuid,
    effective_on date NOT NULL
);
CREATE INDEX policy_agents_idx ON commissions.policy_agents (tenant_id, policy_id, effective_on);

CREATE TABLE commissions.statements (
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL,
    insurer_id  uuid NOT NULL,
    reference   text NOT NULL CHECK (char_length(reference) BETWEEN 1 AND 100),
    received_on date NOT NULL,
    created_at  timestamptz NOT NULL,
    created_by  uuid,
    CONSTRAINT statements_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT statements_reference_key UNIQUE (tenant_id, insurer_id, reference)
);

CREATE TABLE commissions.accruals (
    id                uuid PRIMARY KEY,
    tenant_id         uuid NOT NULL,
    policy_id         uuid NOT NULL,
    kind              text NOT NULL CHECK (kind IN ('payment', 'payment_reversal', 'written', 'endorsement', 'clawback')),
    source_id         uuid NOT NULL,
    base              numeric(19,4) NOT NULL,
    percentage        numeric(9,6) NOT NULL,
    amount            numeric(19,4) NOT NULL,
    currency          char(3) NOT NULL,
    agent_id          uuid,
    accrued_on        date NOT NULL,
    missing_agreement boolean NOT NULL,
    statement_id      uuid,
    created_at        timestamptz NOT NULL,
    CONSTRAINT accruals_source_key UNIQUE (tenant_id, kind, source_id),
    CONSTRAINT accruals_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT accruals_statement_fk FOREIGN KEY (tenant_id, statement_id) REFERENCES commissions.statements (tenant_id, id)
);
CREATE INDEX accruals_policy_idx ON commissions.accruals (tenant_id, policy_id) WHERE statement_id IS NULL;

CREATE TABLE commissions.statement_lines (
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL,
    statement_id  uuid NOT NULL,
    policy_number text NOT NULL CHECK (char_length(policy_number) BETWEEN 1 AND 60),
    policy_id     uuid,
    reported      numeric(19,4) NOT NULL,
    expected      numeric(19,4) NOT NULL,
    matched       boolean NOT NULL,
    closed_at     timestamptz,
    CONSTRAINT lines_statement_fk FOREIGN KEY (tenant_id, statement_id) REFERENCES commissions.statements (tenant_id, id)
);
CREATE INDEX lines_open_idx ON commissions.statement_lines (tenant_id, id) WHERE NOT matched AND closed_at IS NULL;

-- Tasa del asesor (DECISIONS N5): sin ramo es la general.
CREATE TABLE commissions.agent_rates (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    agent_id   uuid NOT NULL,
    line_id    uuid,
    percentage numeric(9,6) NOT NULL CHECK (percentage >= 0 AND percentage <= 100),
    valid_from date NOT NULL,
    valid_to   date,
    created_at timestamptz NOT NULL,
    created_by uuid
);
CREATE INDEX agent_rates_idx ON commissions.agent_rates (tenant_id, agent_id, valid_from);

CREATE TABLE commissions.agent_commissions (
    id           uuid PRIMARY KEY,
    tenant_id    uuid NOT NULL,
    accrual_id   uuid NOT NULL,
    agent_id     uuid NOT NULL,
    percentage   numeric(9,6) NOT NULL,
    amount       numeric(19,4) NOT NULL,
    currency     char(3) NOT NULL,
    missing_rate boolean NOT NULL,
    created_at   timestamptz NOT NULL,
    CONSTRAINT agent_commissions_accrual_key UNIQUE (tenant_id, accrual_id),
    CONSTRAINT agent_commissions_accrual_fk FOREIGN KEY (tenant_id, accrual_id) REFERENCES commissions.accruals (tenant_id, id)
);
CREATE INDEX agent_commissions_agent_idx ON commissions.agent_commissions (tenant_id, agent_id, id);

-- Tolerancia de conciliación por póliza (DECISIONS T4).
CREATE TABLE commissions.settings (
    tenant_id  uuid PRIMARY KEY,
    tolerance  numeric(19,4) NOT NULL CHECK (tolerance >= 0),
    updated_at timestamptz NOT NULL,
    updated_by uuid
);

ALTER TABLE commissions.policy_terms ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.policy_terms FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.policy_terms USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE commissions.policy_agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.policy_agents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.policy_agents USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE commissions.statements ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.statements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.statements USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE commissions.accruals ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.accruals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.accruals USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE commissions.statement_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.statement_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.statement_lines USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE commissions.agent_rates ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.agent_rates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.agent_rates USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE commissions.agent_commissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.agent_commissions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.agent_commissions USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE commissions.settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE commissions.settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commissions.settings USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE ON commissions.policy_terms TO seguros_app;
GRANT SELECT, INSERT ON commissions.policy_agents, commissions.statements, commissions.agent_rates, commissions.agent_commissions TO seguros_app;
GRANT SELECT, INSERT ON commissions.accruals TO seguros_app;
GRANT UPDATE (statement_id) ON commissions.accruals TO seguros_app;
GRANT SELECT, INSERT ON commissions.statement_lines TO seguros_app;
GRANT UPDATE (closed_at) ON commissions.statement_lines TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON commissions.settings TO seguros_app;

-- +goose Down
DROP TABLE commissions.settings;
DROP TABLE commissions.agent_commissions;
DROP TABLE commissions.agent_rates;
DROP TABLE commissions.statement_lines;
DROP TABLE commissions.accruals;
DROP TABLE commissions.statements;
DROP TABLE commissions.policy_agents;
DROP TABLE commissions.policy_terms;
DROP SCHEMA commissions;
