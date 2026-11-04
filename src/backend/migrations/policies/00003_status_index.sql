-- Listado de pólizas filtrado por estado (ListPolicies), keyset por id.
-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY policies_tenant_status_id_idx ON policies.policies (tenant_id, status, id DESC);

-- +goose Down
DROP INDEX CONCURRENTLY policies.policies_tenant_status_id_idx;
