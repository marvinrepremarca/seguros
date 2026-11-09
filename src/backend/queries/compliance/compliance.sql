-- name: InsertKycCheck :exec
INSERT INTO compliance.kyc_checks (id, tenant_id, party_id, document_bidx, result_ciphertext, source_ciphertext, checked_on, details_ciphertext, created_at, created_by)
VALUES (@id, @tenant_id, @party_id, @document_bidx, @result_ciphertext, @source_ciphertext, @checked_on, @details_ciphertext, @at, @actor);

-- name: LatestKycCheck :one
SELECT id, party_id, document_bidx, result, source, result_ciphertext, source_ciphertext, checked_on, (details_ciphertext IS NOT NULL)::boolean AS has_details, created_at
FROM compliance.kyc_checks WHERE tenant_id = @tenant_id AND party_id = @party_id ORDER BY id DESC LIMIT 1;

-- name: ListKycChecks :many
SELECT id, party_id, document_bidx, result, source, result_ciphertext, source_ciphertext, checked_on, (details_ciphertext IS NOT NULL)::boolean AS has_details, created_at
FROM compliance.kyc_checks WHERE tenant_id = @tenant_id AND party_id = @party_id ORDER BY id DESC LIMIT 200;

-- name: GetKycDetails :one
SELECT details_ciphertext FROM compliance.kyc_checks WHERE tenant_id = @tenant_id AND party_id = @party_id AND id = @id;

-- name: InsertComplaint :exec
INSERT INTO compliance.complaints (id, tenant_id, party_id, channel, subject, description, status, received_on, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @party_id, @channel, @subject, @description, 'received', @received_on, @at, @actor, @at, @actor, 1);

-- name: GetComplaint :one
SELECT id, party_id, channel, subject, description, status, resolution, received_on, version FROM compliance.complaints WHERE tenant_id = @tenant_id AND id = @id;

-- name: UpdateComplaint :execrows
UPDATE compliance.complaints SET status = @status, resolution = @resolution, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: ListComplaints :many
SELECT id, party_id, channel, subject, description, status, resolution, received_on, version FROM compliance.complaints
WHERE tenant_id = @tenant_id AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid) ORDER BY id DESC LIMIT @page_size;

-- name: InsertSuppression :exec
INSERT INTO compliance.suppression_requests (id, tenant_id, party_id, status, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @party_id, 'requested', @at, @actor, @at, @actor, 1);

-- name: ResolveSuppression :execrows
UPDATE compliance.suppression_requests SET status = @status, resolution = @resolution, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: GetSuppression :one
SELECT status, version FROM compliance.suppression_requests WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListSuppressions :many
SELECT id, party_id, status, resolution, created_at FROM compliance.suppression_requests
WHERE tenant_id = @tenant_id AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid) ORDER BY id DESC LIMIT @page_size;
