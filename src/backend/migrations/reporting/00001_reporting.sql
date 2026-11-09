-- Indicadores (DOMAIN §5). Vistas de lectura sobre los esquemas de los módulos: es la única frontera por
-- la que Reporting toca sus datos (ARCHITECTURE §4.3). security_invoker mantiene el RLS de cada tabla.
-- +goose Up
CREATE SCHEMA reporting;
GRANT USAGE ON SCHEMA reporting TO seguros_app;

CREATE VIEW reporting.policy_facts WITH (security_invoker = true) AS
SELECT p.tenant_id, p.id, p.status, p.currency, p.net_premium, p.issued_at, p.created_at,
       h.party_id AS holder_party_id
FROM policies.policies p
LEFT JOIN policies.policy_parties h ON h.tenant_id = p.tenant_id AND h.policy_id = p.id AND h.role = 'holder';

CREATE VIEW reporting.installment_facts WITH (security_invoker = true) AS
SELECT i.tenant_id, i.due_date, i.amount - i.paid AS balance, i.status, p.currency, p.cancelled
FROM billing.installments i
JOIN billing.plans p ON p.tenant_id = i.tenant_id AND p.id = i.plan_id;

CREATE VIEW reporting.quote_facts WITH (security_invoker = true) AS
SELECT tenant_id, status, created_at FROM quoting.requests;

CREATE VIEW reporting.renewal_facts WITH (security_invoker = true) AS
SELECT tenant_id, status, effective_to FROM renewals.cases;

CREATE VIEW reporting.claim_facts WITH (security_invoker = true) AS
SELECT tenant_id, status, currency, paid_amount, occurred_on, reported_at, updated_at FROM claims.claims;

CREATE VIEW reporting.accrual_facts WITH (security_invoker = true) AS
SELECT tenant_id, currency, amount, accrued_on FROM commissions.accruals;

-- Una línea sin póliza identificada no tiene moneda conocida: no se cuenta como recibida.
CREATE VIEW reporting.statement_facts WITH (security_invoker = true) AS
SELECT l.tenant_id, t.currency, l.reported, s.received_on
FROM commissions.statement_lines l
JOIN commissions.statements s ON s.tenant_id = l.tenant_id AND s.id = l.statement_id
JOIN commissions.policy_terms t ON t.tenant_id = l.tenant_id AND t.policy_id = l.policy_id;

GRANT SELECT ON ALL TABLES IN SCHEMA reporting TO seguros_app;

-- +goose Down
DROP VIEW reporting.statement_facts;
DROP VIEW reporting.accrual_facts;
DROP VIEW reporting.claim_facts;
DROP VIEW reporting.renewal_facts;
DROP VIEW reporting.quote_facts;
DROP VIEW reporting.installment_facts;
DROP VIEW reporting.policy_facts;
DROP SCHEMA reporting;
