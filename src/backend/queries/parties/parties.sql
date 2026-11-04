-- name: InsertParty :exec
INSERT INTO parties.parties (
    id, tenant_id, kind, name, document_type, document_ciphertext, document_bidx, document_last4,
    email, phone, address, city, responsible_agent_id, created_at, created_by, updated_at, updated_by, version
) VALUES (
    @id, @tenant_id, @kind, @name, @document_type, @document_ciphertext, @document_bidx, @document_last4,
    @email, @phone, @address, @city, @responsible_agent_id, @created_at, @actor, @created_at, @actor, 1
);

-- name: GetParty :one
SELECT id, kind, name, document_type, document_last4, email, phone, address, city, responsible_agent_id, created_at, updated_at, version
FROM parties.parties
WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetPartyByDocument :one
SELECT id, kind, name, document_type, document_last4, email, phone, address, city, responsible_agent_id, created_at, updated_at, version
FROM parties.parties
WHERE tenant_id = @tenant_id AND document_type = @document_type AND document_bidx = @document_bidx;

-- name: GetPartyDocument :one
SELECT document_type, document_ciphertext
FROM parties.parties
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListParties :many
SELECT p.id, p.kind, p.name, p.document_type, p.document_last4, p.email, p.phone, p.address, p.city, p.responsible_agent_id, p.created_at, p.updated_at, p.version
FROM parties.parties p
WHERE p.tenant_id = @tenant_id
  AND (sqlc.narg('before')::uuid IS NULL OR p.id < sqlc.narg('before')::uuid)
  AND (sqlc.narg('name_pattern')::text IS NULL OR p.name ILIKE sqlc.narg('name_pattern')::text ESCAPE '\')
  AND (sqlc.narg('visible_to')::uuid IS NULL
       OR p.responsible_agent_id = sqlc.narg('visible_to')::uuid
       OR EXISTS (SELECT 1 FROM parties.policy_scopes s
                  WHERE s.tenant_id = p.tenant_id AND s.agent_id = sqlc.narg('visible_to')::uuid
                    AND p.id = ANY (s.party_ids)))
ORDER BY p.id DESC
LIMIT @page_size;

-- name: ListPartySummaries :many
SELECT id, kind, name
FROM parties.parties
WHERE tenant_id = @tenant_id AND id = ANY (@ids::uuid[]);

-- name: UpdateParty :execrows
UPDATE parties.parties
SET name = @name, email = @email, phone = @phone, address = @address, city = @city,
    responsible_agent_id = @responsible_agent_id,
    document_type = @document_type, document_last4 = @document_last4,
    document_ciphertext = coalesce(sqlc.narg('document_ciphertext')::bytea, document_ciphertext),
    document_bidx = coalesce(sqlc.narg('document_bidx')::bytea, document_bidx),
    updated_at = @updated_at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: InsertConsent :exec
INSERT INTO parties.consents (id, tenant_id, party_id, status, purpose, text_version, consent_date, recorded_at, recorded_by)
VALUES (@id, @tenant_id, @party_id, @status, @purpose, @text_version, @consent_date, @recorded_at, @recorded_by);

-- name: ListConsents :many
SELECT id, status, purpose, text_version, consent_date, recorded_at
FROM parties.consents
WHERE tenant_id = @tenant_id AND party_id = @party_id
ORDER BY id DESC
LIMIT 500;

-- name: IsPartyVisibleTo :one
SELECT EXISTS (
    SELECT 1 FROM parties.parties p
    WHERE p.tenant_id = @tenant_id AND p.id = @id
      AND (p.responsible_agent_id = @agent::uuid
           OR EXISTS (SELECT 1 FROM parties.policy_scopes s
                      WHERE s.tenant_id = p.tenant_id AND s.agent_id = @agent::uuid AND p.id = ANY (s.party_ids)))
)::boolean AS visible;

-- name: UpsertPolicyScope :exec
INSERT INTO parties.policy_scopes (tenant_id, policy_id, agent_id, party_ids, policy_version, updated_at)
VALUES (@tenant_id, @policy_id, @agent_id, @party_ids, @policy_version, @updated_at)
ON CONFLICT (tenant_id, policy_id) DO UPDATE
SET agent_id = EXCLUDED.agent_id, party_ids = EXCLUDED.party_ids,
    policy_version = EXCLUDED.policy_version, updated_at = EXCLUDED.updated_at
WHERE parties.policy_scopes.policy_version < EXCLUDED.policy_version;

-- name: TransferResponsibleAgent :execrows
WITH moved AS (
    UPDATE parties.parties
    SET responsible_agent_id = @to_agent, updated_at = @updated_at, updated_by = @actor, version = version + 1
    WHERE tenant_id = @tenant_id AND responsible_agent_id = @from_agent
      AND NOT EXISTS (
          SELECT 1 FROM parties.party_responsible_history h
          WHERE h.tenant_id = parties.parties.tenant_id AND h.party_id = parties.parties.id
            AND h.new_agent_id = @from_agent AND h.changed_at > @requested_at)
    RETURNING id, version
)
INSERT INTO parties.party_responsible_history (tenant_id, party_id, party_version, previous_agent_id, new_agent_id, reason, changed_by, changed_at)
SELECT @tenant_id, id, version, @from_agent, @to_agent, @reason, @actor, @updated_at FROM moved;

-- name: InsertResponsibleHistory :exec
INSERT INTO parties.party_responsible_history (tenant_id, party_id, party_version, previous_agent_id, new_agent_id, reason, note, changed_by, changed_at)
VALUES (@tenant_id, @party_id, @party_version, @previous_agent_id, @new_agent_id, @reason, @note, @actor, @changed_at);
