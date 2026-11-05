-- name: InsertInsurer :exec
INSERT INTO catalog.insurers (id, tenant_id, name, active, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @name, @active, @now, @actor, @now, @actor, 1);

-- name: GetInsurer :one
SELECT id, name, active, version FROM catalog.insurers WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListInsurers :many
SELECT id, name, active, version FROM catalog.insurers
WHERE tenant_id = @tenant_id AND (sqlc.narg('after')::uuid IS NULL OR id > sqlc.narg('after')::uuid)
ORDER BY id LIMIT @page_size;

-- name: UpdateInsurer :execrows
UPDATE catalog.insurers SET name = @name, active = @active, updated_at = @now, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: InsertLine :exec
INSERT INTO catalog.lines_of_business (id, tenant_id, name, risk_schema_key, active, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @name, @risk_schema_key, @active, @now, @actor, @now, @actor, 1);

-- name: GetLine :one
SELECT id, name, risk_schema_key, active, version FROM catalog.lines_of_business WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListLines :many
SELECT id, name, risk_schema_key, active, version FROM catalog.lines_of_business
WHERE tenant_id = @tenant_id AND (sqlc.narg('after')::uuid IS NULL OR id > sqlc.narg('after')::uuid)
ORDER BY id LIMIT @page_size;

-- name: UpdateLine :execrows
UPDATE catalog.lines_of_business SET name = @name, active = @active, updated_at = @now, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: InsertProduct :exec
INSERT INTO catalog.products (id, tenant_id, insurer_id, line_of_business_id, name, active, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @insurer_id, @line_of_business_id, @name, @active, @now, @actor, @now, @actor, 1);

-- name: GetProduct :one
SELECT id, insurer_id, line_of_business_id, name, active, version FROM catalog.products WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListProducts :many
SELECT id, insurer_id, line_of_business_id, name, active, version FROM catalog.products
WHERE tenant_id = @tenant_id AND (sqlc.narg('after')::uuid IS NULL OR id > sqlc.narg('after')::uuid)
ORDER BY id LIMIT @page_size;

-- name: UpdateProduct :execrows
UPDATE catalog.products SET name = @name, active = @active, updated_at = @now, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: InsertAgreement :exec
INSERT INTO catalog.commission_agreements (
    id, tenant_id, insurer_id, line_of_business_id, product_id, basis, percentage, valid_from, valid_to,
    created_at, created_by, updated_at, updated_by, version
) VALUES (
    @id, @tenant_id, @insurer_id, @line_of_business_id, @product_id, @basis, @percentage, @valid_from, @valid_to,
    @now, @actor, @now, @actor, 1
);

-- name: GetAgreement :one
SELECT id, insurer_id, line_of_business_id, product_id, basis, percentage, valid_from, valid_to, version
FROM catalog.commission_agreements WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListAgreements :many
SELECT id, insurer_id, line_of_business_id, product_id, basis, percentage, valid_from, valid_to, version
FROM catalog.commission_agreements
WHERE tenant_id = @tenant_id AND (sqlc.narg('after')::uuid IS NULL OR id > sqlc.narg('after')::uuid)
ORDER BY id LIMIT @page_size;

-- name: UpdateAgreement :execrows
UPDATE catalog.commission_agreements
SET basis = @basis, percentage = @percentage, valid_from = @valid_from, valid_to = @valid_to,
    updated_at = @now, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: AgreementFor :one
-- El acuerdo del producto prevalece sobre el general del ramo.
SELECT basis, percentage FROM catalog.commission_agreements
WHERE tenant_id = @tenant_id AND insurer_id = @insurer_id AND line_of_business_id = @line_of_business_id
  AND (product_id = sqlc.narg('product_id')::uuid OR product_id IS NULL)
  AND valid_from <= sqlc.arg('on_date')::date AND valid_to >= sqlc.arg('on_date')::date
ORDER BY product_id NULLS LAST LIMIT 1;
