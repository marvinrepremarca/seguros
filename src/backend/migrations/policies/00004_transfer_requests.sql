-- DECISIONS N12: el motivo es un código del catálogo. La nota libre vive solo aquí y en el historial,
-- nunca en eventos ni en la bitácora. Append-only.
-- +goose Up
ALTER TABLE policies.policy_agent_history ADD COLUMN note text CHECK (char_length(note) <= 200);

CREATE TABLE policies.portfolio_transfer_requests (
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL,
    from_agent_id uuid NOT NULL,
    to_agent_id   uuid NOT NULL,
    reason        text NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 200),
    note          text CHECK (char_length(note) <= 200),
    requested_by  uuid NOT NULL,
    requested_at  timestamptz NOT NULL
);
CREATE INDEX portfolio_transfer_requests_tenant_idx ON policies.portfolio_transfer_requests (tenant_id, id DESC);
ALTER TABLE policies.portfolio_transfer_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.portfolio_transfer_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.portfolio_transfer_requests
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT ON policies.portfolio_transfer_requests TO seguros_app;

-- +goose Down
DROP TABLE policies.portfolio_transfer_requests;
ALTER TABLE policies.policy_agent_history DROP COLUMN note;
