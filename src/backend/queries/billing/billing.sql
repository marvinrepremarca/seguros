-- name: InsertPlan :exec
INSERT INTO billing.plans (id, tenant_id, policy_id, policy_number, insurer_id, holder_party_id, currency, total, credit, cancelled, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @policy_id, @policy_number, @insurer_id, @holder_party_id, @currency, @total, @credit, false, @at, @actor, @at, @actor, 1);

-- name: UpdatePlan :execrows
UPDATE billing.plans
SET total = @total, credit = @credit, cancelled = @cancelled, refund_date = @refund_date, refund_amount = @refund_amount,
    updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: GetPlanByPolicy :one
SELECT id, policy_id, policy_number, insurer_id, holder_party_id, currency, total, credit, cancelled, refund_date, refund_amount, version
FROM billing.plans WHERE tenant_id = @tenant_id AND policy_id = @policy_id;

-- name: ListInstallments :many
SELECT id, number, due_date, amount, paid, status, overdue_notified
FROM billing.installments WHERE tenant_id = @tenant_id AND plan_id = @plan_id ORDER BY number;

-- name: DeleteInstallments :exec
DELETE FROM billing.installments WHERE tenant_id = @tenant_id AND plan_id = @plan_id AND id <> ALL(sqlc.arg('keep')::uuid[]);

-- name: UpsertInstallment :exec
INSERT INTO billing.installments (id, tenant_id, plan_id, number, due_date, amount, paid, status, overdue_notified)
VALUES (@id, @tenant_id, @plan_id, @number, @due_date, @amount, @paid, @status, @overdue_notified)
ON CONFLICT (id) DO UPDATE SET number = EXCLUDED.number, due_date = EXCLUDED.due_date, amount = EXCLUDED.amount,
    paid = EXCLUDED.paid, status = EXCLUDED.status, overdue_notified = EXCLUDED.overdue_notified;

-- name: InsertPayment :exec
INSERT INTO billing.payments (id, tenant_id, plan_id, amount, surplus, currency, paid_on, method, reported_by, support_reference, idempotency_key, request_hash, created_at, created_by)
VALUES (@id, @tenant_id, @plan_id, @amount, @surplus, @currency, @paid_on, @method, @reported_by, @support_reference, @idempotency_key, @request_hash, @created_at, @created_by);

-- name: InsertAllocation :exec
INSERT INTO billing.payment_allocations (tenant_id, payment_id, installment_id, amount) VALUES (@tenant_id, @payment_id, @installment_id, @amount);

-- name: GetPaymentByKey :one
SELECT id, plan_id, amount, surplus, currency, paid_on, method, reported_by, support_reference, request_hash, created_at, reversed_at
FROM billing.payments WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: GetPayment :one
SELECT id, plan_id, amount, surplus, currency, paid_on, method, reported_by, support_reference, request_hash, created_at, reversed_at
FROM billing.payments WHERE tenant_id = @tenant_id AND id = @id AND plan_id = @plan_id;

-- name: ListPayments :many
SELECT id, plan_id, amount, surplus, currency, paid_on, method, reported_by, support_reference, request_hash, created_at, reversed_at
FROM billing.payments WHERE tenant_id = @tenant_id AND plan_id = @plan_id ORDER BY id DESC LIMIT 200;

-- name: ListAllocations :many
SELECT installment_id, amount FROM billing.payment_allocations WHERE tenant_id = @tenant_id AND payment_id = @payment_id;

-- name: MarkPaymentReversed :execrows
UPDATE billing.payments SET reversed_at = @at, reversed_by = @actor, reversal_reason = @reason
WHERE tenant_id = @tenant_id AND id = @id AND reversed_at IS NULL;

-- name: InsertActivity :exec
INSERT INTO billing.collection_activities (id, tenant_id, plan_id, channel, note, created_at, created_by)
VALUES (@id, @tenant_id, @plan_id, @channel, @note, @created_at, @created_by);

-- name: ListActivities :many
SELECT id, channel, note, created_at, created_by FROM billing.collection_activities
WHERE tenant_id = @tenant_id AND plan_id = @plan_id ORDER BY id DESC LIMIT 200;

-- name: ListArrearsSettings :many
SELECT insurer_id, grace_days FROM billing.arrears_settings WHERE tenant_id = @tenant_id ORDER BY insurer_id NULLS FIRST;

-- name: GetGraceDays :one
-- El valor de la aseguradora prevalece sobre el general del tenant.
SELECT grace_days FROM billing.arrears_settings
WHERE tenant_id = @tenant_id AND (insurer_id = @insurer_id OR insurer_id IS NULL)
ORDER BY insurer_id NULLS LAST LIMIT 1;

-- name: UpsertArrearsSetting :exec
INSERT INTO billing.arrears_settings (tenant_id, insurer_id, grace_days, updated_at, updated_by)
VALUES (@tenant_id, @insurer_id, @grace_days, @at, @actor)
ON CONFLICT (tenant_id, insurer_id) DO UPDATE SET grace_days = EXCLUDED.grace_days, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by;

-- name: PoliciesWithDueInstallments :many
SELECT DISTINCT p.policy_id FROM billing.plans p
JOIN billing.installments i ON i.tenant_id = p.tenant_id AND i.plan_id = p.id
WHERE p.tenant_id = @tenant_id AND i.status = 'pending' AND NOT i.overdue_notified AND i.due_date <= @today;

-- name: ListReceivables :many
-- Cuotas con saldo por antigüedad: días de vencida entre min y max. Las no vencidas tienen días negativos.
SELECT i.id, i.due_date, i.amount, i.paid, p.policy_id, p.policy_number, p.insurer_id, p.holder_party_id, p.currency,
       (sqlc.arg('today')::date - i.due_date)::integer AS days_overdue
FROM billing.installments i
JOIN billing.plans p ON p.tenant_id = i.tenant_id AND p.id = i.plan_id
WHERE i.tenant_id = @tenant_id AND i.status = 'pending'
  AND (sqlc.arg('today')::date - i.due_date) BETWEEN sqlc.arg('min_days')::integer AND sqlc.arg('max_days')::integer
  AND (sqlc.narg('after')::uuid IS NULL OR i.id > sqlc.narg('after')::uuid)
ORDER BY i.id
LIMIT @page_size;

-- name: ReceivablesSummary :many
-- Saldo por moneda y tramo de antigüedad (DOMAIN.md §5).
SELECT p.currency,
       (CASE WHEN sqlc.arg('today')::date < i.due_date THEN 'not_due'
             WHEN sqlc.arg('today')::date - i.due_date <= 30 THEN 'd0_30'
             WHEN sqlc.arg('today')::date - i.due_date <= 60 THEN 'd31_60'
             WHEN sqlc.arg('today')::date - i.due_date <= 90 THEN 'd61_90'
             ELSE 'd90_plus' END)::text AS bucket,
       sum(i.amount - i.paid)::numeric AS balance, count(*)::integer AS installments
FROM billing.installments i
JOIN billing.plans p ON p.tenant_id = i.tenant_id AND p.id = i.plan_id
WHERE i.tenant_id = @tenant_id AND i.status = 'pending'
GROUP BY 1, 2 ORDER BY 1, 2;

-- name: TenantsWithDueInstallments :many
SELECT r.tenant_id::uuid AS tenant_id FROM billing.tenants_with_due_installments(sqlc.arg('today')::date) AS r;

-- name: GetLeadDays :one
SELECT lead_days FROM billing.payment_settings WHERE tenant_id = @tenant_id;

-- name: SetLeadDays :exec
INSERT INTO billing.payment_settings (tenant_id, lead_days, updated_at, updated_by) VALUES (@tenant_id, @lead_days, @at, @actor)
ON CONFLICT (tenant_id) DO UPDATE SET lead_days = EXCLUDED.lead_days, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by;
