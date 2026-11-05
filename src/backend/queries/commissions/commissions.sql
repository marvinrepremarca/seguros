-- name: UpsertTerms :exec
INSERT INTO commissions.policy_terms (tenant_id, policy_id, policy_number, insurer_id, line_id, product_id, currency, net_premium, taxes)
VALUES (@tenant_id, @policy_id, @policy_number, @insurer_id, @line_id, @product_id, @currency, @net_premium, @taxes)
ON CONFLICT (tenant_id, policy_id) DO UPDATE SET net_premium = EXCLUDED.net_premium, taxes = EXCLUDED.taxes;

-- name: GetTerms :one
SELECT policy_id, policy_number, insurer_id, line_id, product_id, currency, net_premium, taxes
FROM commissions.policy_terms WHERE tenant_id = @tenant_id AND policy_id = @policy_id;

-- name: FindPolicyByNumber :one
SELECT policy_id FROM commissions.policy_terms
WHERE tenant_id = @tenant_id AND insurer_id = @insurer_id AND policy_number = @policy_number;

-- name: InsertAgent :exec
INSERT INTO commissions.policy_agents (id, tenant_id, policy_id, agent_id, effective_on) VALUES (@id, @tenant_id, @policy_id, @agent_id, @effective_on);

-- name: AgentOn :one
-- El asesor vigente en la fecha; si es anterior al primero registrado, el primero (DECISIONS N16).
SELECT agent_id FROM commissions.policy_agents
WHERE tenant_id = @tenant_id AND policy_id = @policy_id
ORDER BY (effective_on <= sqlc.arg('on_date')::date) DESC,
         CASE WHEN effective_on <= sqlc.arg('on_date')::date THEN effective_on END DESC NULLS LAST,
         effective_on ASC, id DESC
LIMIT 1;

-- name: InsertAccrual :execrows
-- Una misma fuente causa una sola vez.
INSERT INTO commissions.accruals (id, tenant_id, policy_id, kind, source_id, base, percentage, amount, currency, agent_id, accrued_on, missing_agreement, created_at)
VALUES (@id, @tenant_id, @policy_id, @kind, @source_id, @base, @percentage, @amount, @currency, @agent_id, @accrued_on, @missing_agreement, @created_at)
ON CONFLICT (tenant_id, kind, source_id) DO NOTHING;

-- name: GetAccrualBySource :one
SELECT id, policy_id, source_id, base, percentage, amount, currency, agent_id, accrued_on, missing_agreement, statement_id FROM commissions.accruals
WHERE tenant_id = @tenant_id AND kind = @kind AND source_id = @source_id;

-- name: ListAccruals :many
SELECT a.id, a.policy_id, t.policy_number, a.kind, a.base, a.percentage, a.amount, a.currency, a.agent_id, a.accrued_on, a.missing_agreement, a.statement_id
FROM commissions.accruals a
JOIN commissions.policy_terms t ON t.tenant_id = a.tenant_id AND t.policy_id = a.policy_id
WHERE a.tenant_id = @tenant_id
  AND (NOT sqlc.arg('only_pending')::boolean OR a.statement_id IS NULL)
  AND (sqlc.narg('after')::uuid IS NULL OR a.id > sqlc.narg('after')::uuid)
ORDER BY a.id LIMIT @page_size;

-- name: PendingAccruals :many
SELECT a.id, t.line_id, a.source_id, a.kind, a.base, a.percentage, a.amount, a.currency, a.agent_id, a.accrued_on, a.missing_agreement
FROM commissions.accruals a
JOIN commissions.policy_terms t ON t.tenant_id = a.tenant_id AND t.policy_id = a.policy_id
WHERE a.tenant_id = @tenant_id AND a.policy_id = @policy_id AND a.statement_id IS NULL;

-- name: MarkAccrualsReconciled :exec
UPDATE commissions.accruals SET statement_id = @statement_id
WHERE tenant_id = @tenant_id AND id = ANY(sqlc.arg('ids')::uuid[]) AND statement_id IS NULL;

-- name: InsertStatement :exec
INSERT INTO commissions.statements (id, tenant_id, insurer_id, reference, received_on, created_at, created_by)
VALUES (@id, @tenant_id, @insurer_id, @reference, @received_on, @created_at, @created_by);

-- name: InsertLine :exec
INSERT INTO commissions.statement_lines (id, tenant_id, statement_id, policy_number, policy_id, reported, expected, matched)
VALUES (@id, @tenant_id, @statement_id, @policy_number, @policy_id, @reported, @expected, @matched);

-- name: CloseDiscrepancies :exec
-- Una línea nueva de la misma póliza reemplaza las diferencias anteriores: la reclamación vuelve a conciliarse.
UPDATE commissions.statement_lines l SET closed_at = @at
FROM commissions.statements s
WHERE l.tenant_id = @tenant_id AND s.tenant_id = l.tenant_id AND s.id = l.statement_id AND s.insurer_id = @insurer_id
  AND l.policy_number = @policy_number AND NOT l.matched AND l.closed_at IS NULL;

-- name: ListDiscrepancies :many
SELECT l.id, l.statement_id, s.insurer_id, s.reference, l.policy_number, l.policy_id, l.reported, l.expected
FROM commissions.statement_lines l
JOIN commissions.statements s ON s.tenant_id = l.tenant_id AND s.id = l.statement_id
WHERE l.tenant_id = @tenant_id AND NOT l.matched AND l.closed_at IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR l.id > sqlc.narg('after')::uuid)
ORDER BY l.id LIMIT @page_size;

-- name: RateFor :one
-- La del ramo prevalece sobre la general; entre iguales, la más reciente (DECISIONS N5).
SELECT percentage FROM commissions.agent_rates
WHERE tenant_id = @tenant_id AND agent_id = @agent_id AND (line_id = @line_id OR line_id IS NULL)
  AND valid_from <= sqlc.arg('on_date')::date AND (valid_to IS NULL OR valid_to >= sqlc.arg('on_date')::date)
ORDER BY line_id NULLS LAST, valid_from DESC, id DESC LIMIT 1;

-- name: InsertRate :exec
INSERT INTO commissions.agent_rates (id, tenant_id, agent_id, line_id, percentage, valid_from, valid_to, created_at, created_by)
VALUES (@id, @tenant_id, @agent_id, @line_id, @percentage, @valid_from, @valid_to, @created_at, @created_by);

-- name: ListRates :many
SELECT id, agent_id, line_id, percentage, valid_from, valid_to FROM commissions.agent_rates
WHERE tenant_id = @tenant_id ORDER BY id DESC LIMIT 200;

-- name: InsertAgentCommission :exec
INSERT INTO commissions.agent_commissions (id, tenant_id, accrual_id, agent_id, percentage, amount, currency, missing_rate, created_at)
VALUES (@id, @tenant_id, @accrual_id, @agent_id, @percentage, @amount, @currency, @missing_rate, @created_at);

-- name: ListAgentCommissions :many
SELECT id, accrual_id, agent_id, percentage, amount, currency, missing_rate, created_at FROM commissions.agent_commissions
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('after')::uuid IS NULL OR id > sqlc.narg('after')::uuid)
ORDER BY id LIMIT @page_size;

-- name: GetTolerance :one
SELECT tolerance FROM commissions.tolerances WHERE tenant_id = @tenant_id AND currency = @currency;

-- name: ListTolerances :many
SELECT currency, tolerance FROM commissions.tolerances WHERE tenant_id = @tenant_id ORDER BY currency;

-- name: SetTolerance :exec
INSERT INTO commissions.tolerances (tenant_id, currency, tolerance, updated_at, updated_by) VALUES (@tenant_id, @currency, @tolerance, @at, @actor)
ON CONFLICT (tenant_id, currency) DO UPDATE SET tolerance = EXCLUDED.tolerance, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by;
