-- name: IssuedPremiums :many
SELECT currency::text AS currency, COALESCE(sum(net_premium), 0)::numeric AS amount
FROM reporting.policy_facts
WHERE tenant_id = @tenant_id AND issued_at IS NOT NULL
  AND (issued_at AT TIME ZONE 'America/Bogota')::date BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date
GROUP BY currency ORDER BY currency;

-- name: IssuanceDays :one
SELECT COALESCE(round(avg(EXTRACT(EPOCH FROM (issued_at - created_at)) / 86400)::numeric, 1), 0)::numeric AS days
FROM reporting.policy_facts
WHERE tenant_id = @tenant_id AND issued_at IS NOT NULL
  AND (issued_at AT TIME ZONE 'America/Bogota')::date BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date;

-- name: PoliciesPerClient :one
SELECT count(*)::integer AS policies, count(DISTINCT holder_party_id)::integer AS clients
FROM reporting.policy_facts
WHERE tenant_id = @tenant_id AND status IN ('active', 'in_arrears');

-- name: Conversion :one
SELECT count(*)::integer AS requests, count(*) FILTER (WHERE status = 'accepted')::integer AS accepted
FROM reporting.quote_facts
WHERE tenant_id = @tenant_id AND (created_at AT TIME ZONE 'America/Bogota')::date BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date;

-- name: Retention :one
SELECT count(*)::integer AS due, count(*) FILTER (WHERE status = 'renewed')::integer AS renewed
FROM reporting.renewal_facts
WHERE tenant_id = @tenant_id AND effective_to BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date;

-- name: OverdueByAge :many
SELECT (CASE WHEN sqlc.arg('today')::date - due_date <= 30 THEN 'd0_30'
             WHEN sqlc.arg('today')::date - due_date <= 60 THEN 'd31_60'
             WHEN sqlc.arg('today')::date - due_date <= 90 THEN 'd61_90'
             ELSE 'd90_plus' END)::text AS bucket,
       currency::text AS currency, COALESCE(sum(balance), 0)::numeric AS balance
FROM reporting.installment_facts
WHERE tenant_id = @tenant_id AND status = 'pending' AND NOT cancelled AND due_date <= sqlc.arg('today')::date
GROUP BY 1, 2 ORDER BY 1, 2;

-- name: AccruedCommissions :many
SELECT currency::text AS currency, COALESCE(sum(amount), 0)::numeric AS amount
FROM reporting.accrual_facts
WHERE tenant_id = @tenant_id AND accrued_on BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date
GROUP BY currency ORDER BY currency;

-- name: ReceivedCommissions :many
SELECT currency::text AS currency, COALESCE(sum(reported), 0)::numeric AS amount
FROM reporting.statement_facts
WHERE tenant_id = @tenant_id AND received_on BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date
GROUP BY currency ORDER BY currency;

-- name: ClaimDays :one
SELECT COALESCE(round(avg(EXTRACT(EPOCH FROM (updated_at - reported_at)) / 86400)::numeric, 1), 0)::numeric AS days
FROM reporting.claim_facts
WHERE tenant_id = @tenant_id AND status = 'closed'
  AND (updated_at AT TIME ZONE 'America/Bogota')::date BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date;

-- name: PaidClaims :many
SELECT currency::text AS currency, COALESCE(sum(paid_amount), 0)::numeric AS amount
FROM reporting.claim_facts
WHERE tenant_id = @tenant_id AND paid_amount IS NOT NULL AND occurred_on BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date
GROUP BY currency ORDER BY currency;
