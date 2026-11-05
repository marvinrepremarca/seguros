-- name: InsertClaim :exec
INSERT INTO claims.claims (id, tenant_id, policy_id, policy_number, insurer_id, holder_party_id, occurred_on, location, claim_type, description, currency, claimed_amount, status,
    alert_out_of_coverage, alert_in_arrears, alert_not_in_force, reported_at, reported_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @policy_id, @policy_number, @insurer_id, @holder_party_id, @occurred_on, @location, @claim_type, @description, @currency, @claimed_amount, 'reported',
    @alert_out_of_coverage, @alert_in_arrears, @alert_not_in_force, @at, @actor, @at, @actor, 1);

-- name: GetClaim :one
SELECT id, policy_id, policy_number, insurer_id, holder_party_id, occurred_on, location, claim_type, description, currency, claimed_amount, paid_amount, status, insurer_claim_number, decision_support,
    alert_out_of_coverage, alert_in_arrears, alert_not_in_force, (health_ciphertext IS NOT NULL)::boolean AS has_health, reported_at, notified_at, version
FROM claims.claims WHERE tenant_id = @tenant_id AND id = @id;

-- name: UpdateClaim :execrows
UPDATE claims.claims SET status = @status, claimed_amount = @claimed_amount, paid_amount = @paid_amount, insurer_claim_number = @insurer_claim_number, decision_support = @decision_support,
    notified_at = @notified_at, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: ListClaims :many
SELECT id, policy_id, policy_number, insurer_id, holder_party_id, occurred_on, location, claim_type, description, currency, claimed_amount, paid_amount, status, insurer_claim_number, decision_support,
    alert_out_of_coverage, alert_in_arrears, alert_not_in_force, (health_ciphertext IS NOT NULL)::boolean AS has_health, reported_at, notified_at, version
FROM claims.claims
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (NOT @scoped::boolean OR policy_id = ANY(@policies::uuid[]))
  AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid)
ORDER BY id DESC LIMIT @page_size;

-- name: SetHealth :execrows
UPDATE claims.claims SET health_ciphertext = @ciphertext, updated_at = @at, updated_by = @actor, version = version + 1 WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetHealth :one
SELECT health_ciphertext FROM claims.claims WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertChecklistItem :exec
INSERT INTO claims.checklist_items (id, tenant_id, claim_id, label, received, updated_at, updated_by) VALUES (@id, @tenant_id, @claim_id, @label, false, @at, @actor);

-- name: SetChecklistReceived :execrows
UPDATE claims.checklist_items SET received = @received, updated_at = @at, updated_by = @actor WHERE tenant_id = @tenant_id AND claim_id = @claim_id AND id = @id;

-- name: ListChecklist :many
SELECT id, label, received FROM claims.checklist_items WHERE tenant_id = @tenant_id AND claim_id = @claim_id ORDER BY id;

-- name: InsertNote :exec
INSERT INTO claims.notes (id, tenant_id, claim_id, body, created_at, created_by) VALUES (@id, @tenant_id, @claim_id, @body, @at, @actor);

-- name: ListNotes :many
SELECT id, body, created_at, created_by FROM claims.notes WHERE tenant_id = @tenant_id AND claim_id = @claim_id ORDER BY id;
