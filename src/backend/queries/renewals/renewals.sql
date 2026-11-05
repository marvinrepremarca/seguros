-- name: InsertCase :exec
INSERT INTO renewals.cases (id, tenant_id, policy_id, policy_number, insurer_id, holder_party_id, effective_to, status, created_at, updated_at, version)
VALUES (@id, @tenant_id, @policy_id, @policy_number, @insurer_id, @holder_party_id, @effective_to, 'open', @at, @at, 1)
ON CONFLICT (tenant_id, policy_id) DO NOTHING;

-- name: GetCase :one
SELECT id, policy_id, policy_number, insurer_id, holder_party_id, effective_to, status, lost_reason, renewed_by, version
FROM renewals.cases WHERE tenant_id = @tenant_id AND policy_id = @policy_id;

-- name: UpdateCase :execrows
UPDATE renewals.cases SET status = @status, lost_reason = @lost_reason, renewed_by = @renewed_by, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND policy_id = @policy_id AND version = @version;

-- name: ListCases :many
SELECT id, policy_id, policy_number, insurer_id, holder_party_id, effective_to, status, lost_reason, renewed_by, version
FROM renewals.cases
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('after')::uuid IS NULL OR id > sqlc.narg('after')::uuid)
ORDER BY id LIMIT @page_size;

-- name: OpenCasesUntil :many
SELECT policy_id, effective_to FROM renewals.cases
WHERE tenant_id = @tenant_id AND status IN ('open', 'in_progress') AND effective_to <= sqlc.arg('until')::date AND effective_to >= sqlc.arg('today')::date;

-- name: SentAlerts :many
SELECT days FROM renewals.alerts WHERE tenant_id = @tenant_id AND policy_id = @policy_id;

-- name: InsertAlert :exec
INSERT INTO renewals.alerts (tenant_id, policy_id, days, sent_at) VALUES (@tenant_id, @policy_id, @days, @sent_at) ON CONFLICT DO NOTHING;

-- name: GetAlertDays :one
SELECT alert_days FROM renewals.settings WHERE tenant_id = @tenant_id;

-- name: SetAlertDays :exec
INSERT INTO renewals.settings (tenant_id, alert_days, updated_at, updated_by) VALUES (@tenant_id, @alert_days, @at, @actor)
ON CONFLICT (tenant_id) DO UPDATE SET alert_days = EXCLUDED.alert_days, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by;

-- name: Retention :one
-- Tasa de retención (flujos.md Flujo 6): renovadas / por vencer. Las canceladas no cuentan como por vencer.
SELECT count(*)::integer AS due, (count(*) FILTER (WHERE status = 'renewed'))::integer AS renewed
FROM renewals.cases
WHERE tenant_id = @tenant_id AND effective_to BETWEEN sqlc.arg('from_date')::date AND sqlc.arg('to_date')::date AND status <> 'closed';

-- name: TenantsWithOpenCases :many
SELECT r.tenant_id::uuid AS tenant_id FROM renewals.tenants_with_open_cases(sqlc.arg('until')::date) AS r;
