-- Pólizas de cada agencia (ROADMAP Fase 2, DOMAIN.md §2). Una póliza agrupa partes, coberturas,
-- bienes asegurados y endosos. Las referencias a parties, catalog e identity son lógicas (sin FK):
-- un módulo no toca las tablas de otro; se validan por sus contracts.
-- Vigencia [effective_from, effective_to], ambos inclusive (DECISIONS N7).
-- +goose Up
CREATE SCHEMA policies;
GRANT USAGE ON SCHEMA policies TO seguros_app;

CREATE TABLE policies.policies (
    id                     uuid PRIMARY KEY,
    tenant_id              uuid NOT NULL,
    status                 text NOT NULL CHECK (status IN ('in_quotation', 'in_process', 'active', 'in_arrears', 'cancelled', 'expired', 'renewed', 'not_renewed', 'rejected')),
    policy_number          text CHECK (char_length(policy_number) BETWEEN 1 AND 60),
    insurer_id             uuid NOT NULL,
    line_of_business_id    uuid NOT NULL,
    product_id             uuid,
    agent_id               uuid,
    currency               char(3) NOT NULL,
    net_premium            numeric(19,4) NOT NULL CHECK (net_premium >= 0),
    taxes                  numeric(19,4) NOT NULL CHECK (taxes >= 0),
    effective_from         date NOT NULL,
    effective_to           date NOT NULL,
    payment_form           text NOT NULL CHECK (payment_form IN ('single_payment', 'monthly_installments', 'quarterly_installments', 'automatic_debit', 'premium_financing')),
    renewed_from_policy_id uuid,
    issued_at              timestamptz,
    rejection_reason       text CHECK (char_length(rejection_reason) <= 500),
    cancellation_cause     text CHECK (cancellation_cause IN ('client_request', 'item_sold', 'arrears', 'insurer_revocation', 'other')),
    cancellation_date      date,
    unearned_premium       numeric(19,4) CHECK (unearned_premium >= 0),
    cancellation_reference text CHECK (char_length(cancellation_reference) <= 60),
    created_at             timestamptz NOT NULL,
    created_by             uuid,
    updated_at             timestamptz NOT NULL,
    updated_by             uuid,
    version                integer NOT NULL CHECK (version > 0),
    CONSTRAINT policies_period_chk CHECK (effective_to > effective_from),
    CONSTRAINT policies_number_when_issued_chk CHECK (status IN ('in_quotation', 'in_process', 'rejected') OR policy_number IS NOT NULL),
    CONSTRAINT policies_tenant_id_key UNIQUE (tenant_id, id)
);
CREATE UNIQUE INDEX policies_number_key ON policies.policies (tenant_id, insurer_id, policy_number) WHERE policy_number IS NOT NULL;
CREATE INDEX policies_tenant_keyset_idx ON policies.policies (tenant_id, id DESC);
CREATE INDEX policies_tenant_agent_idx ON policies.policies (tenant_id, agent_id, id DESC);
CREATE INDEX policies_expirable_idx ON policies.policies (effective_to) WHERE status = 'active';

CREATE TABLE policies.policy_parties (
    tenant_id uuid NOT NULL,
    policy_id uuid NOT NULL,
    party_id  uuid NOT NULL,
    role      text NOT NULL CHECK (role IN ('holder', 'insured', 'beneficiary')),
    PRIMARY KEY (tenant_id, policy_id, party_id, role),
    CONSTRAINT policy_parties_policy_fk FOREIGN KEY (tenant_id, policy_id) REFERENCES policies.policies (tenant_id, id)
);

CREATE TABLE policies.coverages (
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL,
    policy_id   uuid NOT NULL,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 200),
    sum_insured numeric(19,4) NOT NULL CHECK (sum_insured >= 0),
    deductible  text CHECK (char_length(deductible) <= 200),
    CONSTRAINT coverages_policy_fk FOREIGN KEY (tenant_id, policy_id) REFERENCES policies.policies (tenant_id, id)
);
CREATE INDEX coverages_policy_idx ON policies.coverages (tenant_id, policy_id);

CREATE TABLE policies.insured_items (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL,
    policy_id           uuid NOT NULL,
    description         text NOT NULL CHECK (char_length(description) BETWEEN 2 AND 200),
    risk_schema_key     text NOT NULL CHECK (char_length(risk_schema_key) <= 40),
    risk_schema_version integer NOT NULL CHECK (risk_schema_version > 0),
    risk_data           jsonb NOT NULL CHECK (jsonb_typeof(risk_data) = 'object'),
    CONSTRAINT insured_items_policy_fk FOREIGN KEY (tenant_id, policy_id) REFERENCES policies.policies (tenant_id, id)
);
CREATE INDEX insured_items_policy_idx ON policies.insured_items (tenant_id, policy_id);

CREATE TABLE policies.endorsements (
    id                uuid PRIMARY KEY,
    tenant_id         uuid NOT NULL,
    policy_id         uuid NOT NULL,
    type              text NOT NULL CHECK (type IN ('data_change', 'item_change', 'party_inclusion', 'party_exclusion', 'sum_insured_increase', 'sum_insured_decrease', 'other')),
    status            text NOT NULL CHECK (status IN ('requested', 'issued', 'rejected')),
    description       text NOT NULL CHECK (char_length(description) BETWEEN 3 AND 500),
    effective_date    date NOT NULL,
    requested_at      timestamptz NOT NULL,
    requested_by      uuid,
    resolved_at       timestamptz,
    endorsement_number text CHECK (char_length(endorsement_number) <= 60),
    net_premium_delta numeric(19,4),
    taxes_delta       numeric(19,4),
    effects           jsonb,
    rejection_reason  text CHECK (char_length(rejection_reason) <= 500),
    CONSTRAINT endorsements_policy_fk FOREIGN KEY (tenant_id, policy_id) REFERENCES policies.policies (tenant_id, id)
);
CREATE INDEX endorsements_policy_idx ON policies.endorsements (tenant_id, policy_id, id);

ALTER TABLE policies.policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.policies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.policies
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
-- Solo para la función SECURITY DEFINER de abajo, que corre como dueño.
CREATE POLICY owner_expiration_scan ON policies.policies FOR SELECT TO CURRENT_USER USING (true);

ALTER TABLE policies.policy_parties ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.policy_parties FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.policy_parties
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

ALTER TABLE policies.coverages ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.coverages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.coverages
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

ALTER TABLE policies.insured_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.insured_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.insured_items
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

ALTER TABLE policies.endorsements ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.endorsements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.endorsements
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE ON policies.policies TO seguros_app;
GRANT SELECT, INSERT, DELETE ON policies.policy_parties TO seguros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON policies.coverages TO seguros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON policies.insured_items TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON policies.endorsements TO seguros_app;

-- Pólizas Vigentes cuyo último día cubierto ya pasó, de todos los tenants (DECISIONS N7). Solo expone
-- IDs, y solo al worker: la transición ocurre después, bajo RLS del tenant de cada póliza.
-- +goose StatementBegin
CREATE FUNCTION policies.due_for_expiration(p_today date, p_limit integer)
RETURNS TABLE (tenant_id uuid, id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT p.tenant_id, p.id
    FROM policies.policies p
    WHERE p.status = 'active' AND p.effective_to < p_today
    ORDER BY p.effective_to, p.id
    LIMIT least(greatest(p_limit, 1), 1000)
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION policies.due_for_expiration(date, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION policies.due_for_expiration(date, integer) TO seguros_worker;

-- +goose Down
DROP FUNCTION policies.due_for_expiration(date, integer);
DROP TABLE policies.endorsements;
DROP TABLE policies.insured_items;
DROP TABLE policies.coverages;
DROP TABLE policies.policy_parties;
DROP TABLE policies.policies;
DROP SCHEMA policies;
