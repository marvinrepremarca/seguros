-- name: InsertPolicy :exec
INSERT INTO policies.policies (
    id, tenant_id, status, policy_number, insurer_id, line_of_business_id, product_id, agent_id, currency,
    net_premium, taxes, effective_from, effective_to, payment_form, renewed_from_policy_id, issued_at,
    rejection_reason, cancellation_cause, cancellation_date, unearned_premium, cancellation_reference,
    created_at, created_by, updated_at, updated_by, version
) VALUES (
    @id, @tenant_id, @status, @policy_number, @insurer_id, @line_of_business_id, @product_id, @agent_id, @currency,
    @net_premium, @taxes, @effective_from, @effective_to, @payment_form, @renewed_from_policy_id, @issued_at,
    @rejection_reason, @cancellation_cause, @cancellation_date, @unearned_premium, @cancellation_reference,
    @created_at, @actor, @updated_at, @actor, 1
);

-- name: UpdatePolicy :execrows
UPDATE policies.policies
SET status = @status, policy_number = @policy_number, insurer_id = @insurer_id, line_of_business_id = @line_of_business_id,
    product_id = @product_id, agent_id = @agent_id, currency = @currency, net_premium = @net_premium, taxes = @taxes,
    effective_from = @effective_from, effective_to = @effective_to, payment_form = @payment_form,
    renewed_from_policy_id = @renewed_from_policy_id, issued_at = @issued_at, rejection_reason = @rejection_reason,
    cancellation_cause = @cancellation_cause, cancellation_date = @cancellation_date, unearned_premium = @unearned_premium,
    cancellation_reference = @cancellation_reference, updated_at = @updated_at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: GetPolicy :one
SELECT id, status, policy_number, insurer_id, line_of_business_id, product_id, agent_id, currency, net_premium, taxes,
       effective_from, effective_to, payment_form, renewed_from_policy_id, issued_at, rejection_reason,
       cancellation_cause, cancellation_date, unearned_premium, cancellation_reference, created_at, updated_at, version
FROM policies.policies
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListPolicies :many
SELECT p.id, p.status, p.policy_number, p.insurer_id, p.line_of_business_id, p.currency, p.net_premium, p.taxes,
       p.effective_from, p.effective_to, p.agent_id,
       (SELECT pp.party_id FROM policies.policy_parties pp
        WHERE pp.tenant_id = p.tenant_id AND pp.policy_id = p.id AND pp.role = 'holder' LIMIT 1)::uuid AS holder_party_id
FROM policies.policies p
WHERE p.tenant_id = @tenant_id
  AND (sqlc.narg('before')::uuid IS NULL OR p.id < sqlc.narg('before')::uuid)
  AND (sqlc.narg('agent_id')::uuid IS NULL OR p.agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR p.status = sqlc.narg('status')::text)
ORDER BY p.id DESC
LIMIT @page_size;

-- name: ListPolicyParties :many
SELECT party_id, role FROM policies.policy_parties WHERE tenant_id = @tenant_id AND policy_id = @policy_id ORDER BY role, party_id;

-- name: InsertPolicyParty :exec
INSERT INTO policies.policy_parties (tenant_id, policy_id, party_id, role) VALUES (@tenant_id, @policy_id, @party_id, @role);

-- name: DeletePolicyParties :exec
DELETE FROM policies.policy_parties WHERE tenant_id = @tenant_id AND policy_id = @policy_id;

-- name: ListCoverages :many
SELECT id, name, sum_insured, deductible FROM policies.coverages WHERE tenant_id = @tenant_id AND policy_id = @policy_id ORDER BY id;

-- name: InsertCoverage :exec
INSERT INTO policies.coverages (id, tenant_id, policy_id, name, sum_insured, deductible)
VALUES (@id, @tenant_id, @policy_id, @name, @sum_insured, @deductible);

-- name: DeleteCoverages :exec
DELETE FROM policies.coverages WHERE tenant_id = @tenant_id AND policy_id = @policy_id;

-- name: ListInsuredItems :many
SELECT id, description, risk_schema_key, risk_schema_version, risk_data FROM policies.insured_items
WHERE tenant_id = @tenant_id AND policy_id = @policy_id ORDER BY id;

-- name: InsertInsuredItem :exec
INSERT INTO policies.insured_items (id, tenant_id, policy_id, description, risk_schema_key, risk_schema_version, risk_data)
VALUES (@id, @tenant_id, @policy_id, @description, @risk_schema_key, @risk_schema_version, @risk_data);

-- name: DeleteInsuredItems :exec
DELETE FROM policies.insured_items WHERE tenant_id = @tenant_id AND policy_id = @policy_id;

-- name: ListEndorsements :many
SELECT id, type, status, description, effective_date, requested_at, resolved_at, endorsement_number,
       net_premium_delta, taxes_delta, effects, rejection_reason
FROM policies.endorsements
WHERE tenant_id = @tenant_id AND policy_id = @policy_id ORDER BY id;

-- name: UpsertEndorsement :exec
INSERT INTO policies.endorsements (
    id, tenant_id, policy_id, type, status, description, effective_date, requested_at, requested_by, resolved_at,
    endorsement_number, net_premium_delta, taxes_delta, effects, rejection_reason
) VALUES (
    @id, @tenant_id, @policy_id, @type, @status, @description, @effective_date, @requested_at, @requested_by, @resolved_at,
    @endorsement_number, @net_premium_delta, @taxes_delta, @effects, @rejection_reason
)
ON CONFLICT (id) DO UPDATE
SET status = EXCLUDED.status, resolved_at = EXCLUDED.resolved_at, endorsement_number = EXCLUDED.endorsement_number,
    net_premium_delta = EXCLUDED.net_premium_delta, taxes_delta = EXCLUDED.taxes_delta,
    effects = coalesce(EXCLUDED.effects, policies.endorsements.effects), rejection_reason = EXCLUDED.rejection_reason;

-- name: DueForExpiration :many
SELECT r.tenant_id::uuid AS tenant_id, r.id::uuid AS id
FROM policies.due_for_expiration(@today::date, @page_size::integer) AS r;

-- name: InsertAgentHistory :exec
INSERT INTO policies.policy_agent_history (id, tenant_id, policy_id, previous_agent_id, agent_id, effective_at, reason, changed_by)
VALUES (@id, @tenant_id, @policy_id, @previous_agent_id, @agent_id, @effective_at, @reason, @changed_by);

-- name: ListReassignablePolicyIDs :many
SELECT id FROM policies.policies
WHERE tenant_id = @tenant_id AND agent_id = @agent_id
  AND status IN ('in_quotation', 'in_process', 'active', 'in_arrears', 'expired')
ORDER BY id
LIMIT @page_size;
