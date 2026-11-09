-- name: InsertCall :exec
INSERT INTO integrations.calls (id, tenant_id, connector, operation, outcome, created_at, created_by)
VALUES (@id, @tenant_id, @connector, @operation, @outcome, @at, sqlc.narg('actor'));
