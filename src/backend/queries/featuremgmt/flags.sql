-- name: ListTenantOverrides :many
SELECT flag_key, enabled, reason, updated_at, version
FROM featuremgmt.tenant_overrides
WHERE tenant_id = $1
ORDER BY flag_key;

-- name: UpsertTenantOverride :exec
INSERT INTO featuremgmt.tenant_overrides
    (tenant_id, flag_key, enabled, reason, created_at, created_by, updated_at, updated_by, version)
VALUES
    (@tenant_id, @flag_key, @enabled, @reason, @now, @actor, @now, @actor, 1)
ON CONFLICT (tenant_id, flag_key) DO UPDATE
SET enabled = EXCLUDED.enabled, reason = EXCLUDED.reason, updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by, version = featuremgmt.tenant_overrides.version + 1;

-- name: DeleteTenantOverride :execrows
DELETE FROM featuremgmt.tenant_overrides
WHERE tenant_id = $1 AND flag_key = $2;

-- name: ListGlobalSwitches :many
SELECT flag_key, enabled FROM featuremgmt.global_switches ORDER BY flag_key;

-- name: GetGlobalSwitchForUpdate :one
SELECT enabled FROM featuremgmt.global_switches WHERE flag_key = $1 FOR UPDATE;

-- name: UpsertGlobalSwitch :exec
INSERT INTO featuremgmt.global_switches (flag_key, enabled, reason, updated_at, updated_by)
VALUES (@flag_key, @enabled, @reason, @now, @operator)
ON CONFLICT (flag_key) DO UPDATE
SET enabled = EXCLUDED.enabled, reason = EXCLUDED.reason, updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by;

-- name: InsertGlobalSwitchHistory :exec
INSERT INTO featuremgmt.global_switch_history (id, flag_key, previous, new, reason, operator, changed_at)
VALUES (@id, @flag_key, sqlc.narg('previous'), sqlc.narg('new'), @reason, @operator, @now);

-- name: DeleteGlobalSwitch :execrows
DELETE FROM featuremgmt.global_switches WHERE flag_key = $1;
