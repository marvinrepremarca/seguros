-- Cartera y cobro (Flujo 4). La agencia no custodia primas (DECISIONS N1): los pagos son informados.
-- Los pagos son inmutables: la app solo puede escribir las columnas del reverso.
-- +goose Up
CREATE SCHEMA billing;
GRANT USAGE ON SCHEMA billing TO seguros_app;

CREATE TABLE billing.plans (
    id                 uuid PRIMARY KEY,
    tenant_id          uuid NOT NULL,
    policy_id          uuid NOT NULL,
    policy_number      text NOT NULL CHECK (char_length(policy_number) <= 60),
    insurer_id         uuid NOT NULL,
    holder_party_id    uuid NOT NULL,
    currency           char(3) NOT NULL,
    total              numeric(19,4) NOT NULL CHECK (total >= 0),
    credit             numeric(19,4) NOT NULL CHECK (credit >= 0),
    cancelled          boolean NOT NULL,
    refund_date        date,
    refund_amount      numeric(19,4) CHECK (refund_amount >= 0),
    created_at         timestamptz NOT NULL,
    created_by         uuid,
    updated_at         timestamptz NOT NULL,
    updated_by         uuid,
    version            integer NOT NULL CHECK (version > 0),
    CONSTRAINT plans_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT plans_tenant_policy_key UNIQUE (tenant_id, policy_id),
    CONSTRAINT plans_refund_check CHECK ((refund_date IS NULL) = (refund_amount IS NULL))
);

CREATE TABLE billing.installments (
    id               uuid PRIMARY KEY,
    tenant_id        uuid NOT NULL,
    plan_id          uuid NOT NULL,
    number           integer NOT NULL CHECK (number > 0),
    due_date         date NOT NULL,
    amount           numeric(19,4) NOT NULL CHECK (amount >= 0),
    paid             numeric(19,4) NOT NULL CHECK (paid >= 0),
    status           text NOT NULL CHECK (status IN ('pending', 'paid', 'voided')),
    overdue_notified boolean NOT NULL,
    CONSTRAINT installments_plan_fk FOREIGN KEY (tenant_id, plan_id) REFERENCES billing.plans (tenant_id, id)
);
CREATE INDEX installments_plan_idx ON billing.installments (tenant_id, plan_id);
CREATE INDEX installments_pending_idx ON billing.installments (tenant_id, due_date, id) WHERE status = 'pending';

CREATE TABLE billing.payments (
    id                uuid PRIMARY KEY,
    tenant_id         uuid NOT NULL,
    plan_id           uuid NOT NULL,
    amount            numeric(19,4) NOT NULL CHECK (amount > 0),
    surplus           numeric(19,4) NOT NULL CHECK (surplus >= 0),
    currency          char(3) NOT NULL,
    paid_on           date NOT NULL,
    method            text NOT NULL CHECK (method IN ('transfer', 'card', 'cash', 'check', 'automatic_debit', 'financing', 'other')),
    reported_by       text NOT NULL CHECK (reported_by IN ('client', 'insurer')),
    support_reference text NOT NULL CHECK (char_length(support_reference) BETWEEN 1 AND 200),
    idempotency_key   text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 100),
    request_hash      text NOT NULL,
    created_at        timestamptz NOT NULL,
    created_by        uuid,
    reversed_at       timestamptz,
    reversed_by       uuid,
    reversal_reason   text CHECK (char_length(reversal_reason) <= 500),
    CONSTRAINT payments_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT payments_idempotency_key UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT payments_plan_fk FOREIGN KEY (tenant_id, plan_id) REFERENCES billing.plans (tenant_id, id)
);
CREATE INDEX payments_plan_idx ON billing.payments (tenant_id, plan_id, id DESC);

CREATE TABLE billing.payment_allocations (
    tenant_id      uuid NOT NULL,
    payment_id     uuid NOT NULL,
    installment_id uuid NOT NULL,
    amount         numeric(19,4) NOT NULL CHECK (amount > 0),
    PRIMARY KEY (tenant_id, payment_id, installment_id),
    CONSTRAINT allocations_payment_fk FOREIGN KEY (tenant_id, payment_id) REFERENCES billing.payments (tenant_id, id)
);

-- Días de gracia (DECISIONS N2): insurer_id NULL es el valor general del tenant.
CREATE TABLE billing.arrears_settings (
    tenant_id  uuid NOT NULL,
    insurer_id uuid,
    grace_days integer NOT NULL CHECK (grace_days BETWEEN 0 AND 365),
    updated_at timestamptz NOT NULL,
    updated_by uuid,
    CONSTRAINT arrears_settings_key UNIQUE NULLS NOT DISTINCT (tenant_id, insurer_id)
);

CREATE TABLE billing.collection_activities (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    plan_id    uuid NOT NULL,
    channel    text NOT NULL CHECK (channel IN ('call', 'message', 'email', 'visit', 'other')),
    note       text NOT NULL CHECK (char_length(note) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL,
    created_by uuid,
    CONSTRAINT activities_plan_fk FOREIGN KEY (tenant_id, plan_id) REFERENCES billing.plans (tenant_id, id)
);
CREATE INDEX activities_plan_idx ON billing.collection_activities (tenant_id, plan_id, id DESC);

ALTER TABLE billing.plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing.plans FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing.plans
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE billing.installments ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing.installments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing.installments
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE billing.payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing.payments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing.payments
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE billing.payment_allocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing.payment_allocations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing.payment_allocations
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE billing.arrears_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing.arrears_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing.arrears_settings
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE billing.collection_activities ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing.collection_activities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing.collection_activities
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE ON billing.plans TO seguros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON billing.installments TO seguros_app;
GRANT SELECT, INSERT ON billing.payments TO seguros_app;
GRANT UPDATE (reversed_at, reversed_by, reversal_reason) ON billing.payments TO seguros_app;
GRANT SELECT, INSERT ON billing.payment_allocations TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON billing.arrears_settings TO seguros_app;
GRANT SELECT, INSERT ON billing.collection_activities TO seguros_app;

-- Tenants con cuotas vencidas sin avisar. Cruza tenants: solo devuelve IDs y solo la ejecuta el worker;
-- la evaluación de días de gracia ocurre después, bajo RLS de cada tenant y con su flag verificado.
CREATE POLICY owner_scan ON billing.installments FOR SELECT TO CURRENT_USER USING (true);
-- +goose StatementBegin
CREATE FUNCTION billing.tenants_with_due_installments(p_today date)
RETURNS TABLE (tenant_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT DISTINCT i.tenant_id FROM billing.installments i
    WHERE i.status = 'pending' AND NOT i.overdue_notified AND i.due_date <= p_today
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION billing.tenants_with_due_installments(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION billing.tenants_with_due_installments(date) TO seguros_worker;

-- +goose Down
DROP FUNCTION billing.tenants_with_due_installments(date);
DROP TABLE billing.collection_activities;
DROP TABLE billing.arrears_settings;
DROP TABLE billing.payment_allocations;
DROP TABLE billing.payments;
DROP TABLE billing.installments;
DROP TABLE billing.plans;
DROP SCHEMA billing;
