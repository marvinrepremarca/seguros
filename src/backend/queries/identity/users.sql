-- name: GetUserBySubject :one
SELECT id, tenant_id, idp_subject, email, display_name, last_login_at, created_at, updated_at, version
FROM identity.users
WHERE tenant_id = $1 AND idp_subject = $2;

-- name: InsertUser :exec
INSERT INTO identity.users
    (id, tenant_id, idp_subject, email, display_name, last_login_at, created_at, created_by, updated_at, updated_by, version)
VALUES
    (@id, @tenant_id, @idp_subject, @email, @display_name, @last_login_at, @last_login_at, @id, @last_login_at, @id, 1);

-- name: UpdateUserLogin :execrows
UPDATE identity.users
SET email = @email, display_name = @display_name, last_login_at = @last_login_at, left_organization_at = NULL,
    updated_at = @last_login_at, updated_by = id, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: ListUsers :many
SELECT id, idp_subject, display_name, left_organization_at FROM identity.users WHERE tenant_id = @tenant_id ORDER BY display_name, id LIMIT 500;

-- name: MarkUserLeftOrganization :exec
UPDATE identity.users
SET left_organization_at = @left_at, updated_at = @left_at, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND left_organization_at IS NULL;

-- name: GetUserByID :one
SELECT id, idp_subject, display_name, left_organization_at FROM identity.users WHERE tenant_id = @tenant_id AND id = @id;
