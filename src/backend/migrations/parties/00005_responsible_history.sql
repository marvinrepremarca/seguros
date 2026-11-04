-- DECISIONS N11: quién fue responsable de cada cliente y cuándo, también en un traspaso en bloque, para
-- revertir uno equivocado y responder quién tuvo acceso (Ley 1581/2012). Append-only. La versión es la
-- que quedó en el cliente tras el cambio.
-- +goose Up
CREATE TABLE parties.party_responsible_history (
    tenant_id         uuid NOT NULL,
    party_id          uuid NOT NULL REFERENCES parties.parties (id),
    party_version     integer NOT NULL,
    previous_agent_id uuid,
    new_agent_id      uuid NOT NULL,
    reason            text NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 200),
    changed_by        uuid,
    changed_at        timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, party_id, party_version)
);
ALTER TABLE parties.party_responsible_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE parties.party_responsible_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON parties.party_responsible_history
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT ON parties.party_responsible_history TO seguros_app;

-- +goose Down
DROP TABLE parties.party_responsible_history;
