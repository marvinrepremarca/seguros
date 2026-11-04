-- Alcance del asesor (DECISIONS N10): su cliente es quien registró o una parte de sus pólizas. Parties
-- no lee tablas de Policies: esta proyección local se alimenta del evento PolicyPartiesChanged.
-- policy_version descarta entregas viejas o fuera de orden (el outbox es at-least-once).
-- +goose Up
CREATE TABLE parties.policy_scopes (
    tenant_id      uuid NOT NULL,
    policy_id      uuid NOT NULL,
    agent_id       uuid,
    party_ids      uuid[] NOT NULL,
    policy_version integer NOT NULL CHECK (policy_version > 0),
    updated_at     timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, policy_id)
);
CREATE INDEX policy_scopes_agent_idx ON parties.policy_scopes (tenant_id, agent_id) WHERE agent_id IS NOT NULL;
CREATE INDEX parties_tenant_created_by_idx ON parties.parties (tenant_id, created_by);

ALTER TABLE parties.policy_scopes ENABLE ROW LEVEL SECURITY;
ALTER TABLE parties.policy_scopes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON parties.policy_scopes
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON parties.policy_scopes TO seguros_app;

-- +goose Down
DROP INDEX parties.parties_tenant_created_by_idx;
DROP TABLE parties.policy_scopes;
