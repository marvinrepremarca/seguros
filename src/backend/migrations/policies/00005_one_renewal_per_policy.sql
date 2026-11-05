-- Una póliza tiene una sola renovación en curso o expedida: dos solicitudes simultáneas no la duplican.
-- +goose Up
CREATE UNIQUE INDEX policies_one_renewal_key ON policies.policies (tenant_id, renewed_from_policy_id)
    WHERE renewed_from_policy_id IS NOT NULL AND status NOT IN ('rejected', 'cancelled');

-- +goose Down
DROP INDEX policies.policies_one_renewal_key;
