-- name: InsertTenant :exec
INSERT INTO tenancy.tenants (
    id, name, plan, status, idp_org_alias, status_since, created_at, created_by, updated_at, updated_by, version
) VALUES (
    @id, @name, @plan, @status, @idp_org_alias, @status_since, @created_at, @created_by, @updated_at, @updated_by, @version
);

-- name: GetTenant :one
SELECT id, name, plan, status, idp_org_alias, status_since, created_at, updated_at, version
FROM tenancy.tenants
WHERE id = @id;

-- name: UpdateTenant :execrows
UPDATE tenancy.tenants
SET name = @name,
    plan = @plan,
    status = @status,
    status_since = @status_since,
    updated_at = @updated_at,
    updated_by = @updated_by,
    version = version + 1
WHERE id = @id AND version = @version;

-- name: ResolveTenantByOrgAlias :one
SELECT r.id::uuid AS id, r.status::text AS status
FROM tenancy.resolve_tenant_by_org_alias(@idp_org_alias::text) AS r;
