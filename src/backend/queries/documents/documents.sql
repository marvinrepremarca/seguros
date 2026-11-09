-- name: InsertDocument :exec
INSERT INTO documents.documents (id, tenant_id, owner_type, owner_id, title, sensitive, idempotency_key, current_number, created_on, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @owner_type, @owner_id, @title, @sensitive, sqlc.narg('idempotency_key'), 1, @created_on, @at, sqlc.narg('actor'), @at, sqlc.narg('actor'), 1);

-- name: InsertVersion :exec
INSERT INTO documents.versions (id, tenant_id, document_id, number, storage_key, mime_type, size_bytes, sha256, encrypted, scan_status, created_at, created_by)
VALUES (@id, @tenant_id, @document_id, @number, @storage_key, @mime_type, @size_bytes, @sha256, @encrypted, 'pending', @at, sqlc.narg('actor'));

-- name: BumpDocument :execrows
UPDATE documents.documents SET current_number = @number, updated_at = @at, updated_by = sqlc.narg('actor'), version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: GetDocument :one
SELECT * FROM documents.documents WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetDocumentByKey :one
SELECT * FROM documents.documents WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: ListByOwner :many
SELECT * FROM documents.documents
WHERE tenant_id = @tenant_id AND owner_type = @owner_type AND owner_id = @owner_id
  AND (@include_sensitive::boolean OR NOT sensitive)
  AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid)
ORDER BY id DESC LIMIT @row_limit;

-- name: ListRetentionDue :many
SELECT * FROM documents.documents
WHERE tenant_id = @tenant_id AND created_on <= @created_until AND owner_type = ANY (@owner_types::text[])
  AND (@include_sensitive::boolean OR NOT sensitive)
  AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid)
ORDER BY id DESC LIMIT @row_limit;

-- name: ListVersions :many
SELECT * FROM documents.versions WHERE tenant_id = @tenant_id AND document_id = @document_id ORDER BY number DESC;

-- name: ListPendingScans :many
SELECT * FROM documents.versions WHERE tenant_id = @tenant_id AND scan_status = 'pending' ORDER BY id LIMIT @row_limit;

-- name: SetScanResult :execrows
UPDATE documents.versions SET scan_status = @scan_status, scanned_at = @at
WHERE tenant_id = @tenant_id AND id = @id AND scan_status = 'pending';

-- name: TenantsWithPendingScans :many
SELECT r.tenant_id::uuid AS tenant_id FROM documents.tenants_with_pending_scans() AS r;
