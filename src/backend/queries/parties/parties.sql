-- name: InsertParty :exec
INSERT INTO parties.parties (
    id, tenant_id, kind, name, document_type, document_ciphertext, document_bidx, document_last4,
    email, phone, address, city, created_at, created_by, updated_at, updated_by, version
) VALUES (
    @id, @tenant_id, @kind, @name, @document_type, @document_ciphertext, @document_bidx, @document_last4,
    @email, @phone, @address, @city, @created_at, @actor, @created_at, @actor, 1
);

-- name: GetParty :one
SELECT id, kind, name, document_type, document_last4, email, phone, address, city, created_at, updated_at, version
FROM parties.parties
WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetPartyByDocument :one
SELECT id, kind, name, document_type, document_last4, email, phone, address, city, created_at, updated_at, version
FROM parties.parties
WHERE tenant_id = @tenant_id AND document_type = @document_type AND document_bidx = @document_bidx;

-- name: GetPartyDocument :one
SELECT document_type, document_ciphertext
FROM parties.parties
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListParties :many
SELECT id, kind, name, document_type, document_last4, email, phone, address, city, created_at, updated_at, version
FROM parties.parties
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid)
  AND (sqlc.narg('name_pattern')::text IS NULL OR name ILIKE sqlc.narg('name_pattern')::text ESCAPE '\')
ORDER BY id DESC
LIMIT @page_size;

-- name: ListPartySummaries :many
SELECT id, kind, name
FROM parties.parties
WHERE tenant_id = @tenant_id AND id = ANY (@ids::uuid[]);

-- name: UpdateParty :execrows
UPDATE parties.parties
SET name = @name, email = @email, phone = @phone, address = @address, city = @city,
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
