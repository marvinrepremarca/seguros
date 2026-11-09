-- name: InsertMessage :exec
INSERT INTO notifications.messages (id, tenant_id, template, channel, policy_id, params, dedupe_key, status, attempts, created_at, updated_at, version)
VALUES (@id, @tenant_id, @template, 'email', @policy_id, @params, @dedupe_key, 'pending', 0, @at, @at, 1)
ON CONFLICT (tenant_id, dedupe_key) DO NOTHING;

-- name: ClaimPending :many
-- SKIP LOCKED y el plazo evitan que dos jobs envíen el mismo aviso.
UPDATE notifications.messages AS u SET next_attempt_at = @lease_until
WHERE u.tenant_id = @tenant_id AND u.id IN (
    SELECT m.id FROM notifications.messages m
    WHERE m.tenant_id = @tenant_id AND m.status = 'pending' AND (m.next_attempt_at IS NULL OR m.next_attempt_at <= @now)
    ORDER BY m.id LIMIT @row_limit FOR UPDATE SKIP LOCKED)
RETURNING u.*;

-- name: MarkSent :exec
UPDATE notifications.messages SET status = 'sent', party_id = @party_id, attempts = attempts + 1, sent_at = @at, updated_at = @at, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id;

-- name: MarkSkipped :exec
UPDATE notifications.messages SET status = 'skipped', party_id = @party_id, skip_reason = @skip_reason, updated_at = @at, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id;

-- name: MarkAttempt :exec
UPDATE notifications.messages SET status = @status, attempts = @attempts, next_attempt_at = sqlc.narg('next_attempt_at'), updated_at = @at, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id;

-- name: Retry :execrows
UPDATE notifications.messages SET status = 'pending', attempts = 0, next_attempt_at = NULL, updated_at = @at, updated_by = sqlc.narg('actor'), version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND status = 'failed';

-- name: ListMessages :many
SELECT * FROM notifications.messages
WHERE tenant_id = @tenant_id AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid)
ORDER BY id DESC LIMIT @row_limit;

-- name: TenantsWithPendingMessages :many
SELECT r.tenant_id::uuid AS tenant_id FROM notifications.tenants_with_pending_messages() AS r;
