-- DECISIONS N11: historial de asesores de cada póliza, con fechas. Append-only: la Fase 3 lo necesita
-- para atribuir comisiones cuando hay traspasos.
-- +goose Up
CREATE TABLE policies.policy_agent_history (
    id                uuid PRIMARY KEY,
    tenant_id         uuid NOT NULL,
    policy_id         uuid NOT NULL,
    previous_agent_id uuid,
    agent_id          uuid,
    effective_at      timestamptz NOT NULL,
    reason            text NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 200),
    changed_by        uuid,
    CONSTRAINT policy_agent_history_policy_fk FOREIGN KEY (tenant_id, policy_id) REFERENCES policies.policies (tenant_id, id)
);
CREATE INDEX policy_agent_history_policy_idx ON policies.policy_agent_history (tenant_id, policy_id, effective_at);

ALTER TABLE policies.policy_agent_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies.policy_agent_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies.policy_agent_history
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT ON policies.policy_agent_history TO seguros_app;

-- +goose Down
DROP TABLE policies.policy_agent_history;
