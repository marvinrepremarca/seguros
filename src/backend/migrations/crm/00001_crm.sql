-- CRM (Flujo 1): prospectos, embudo y asesor asignado.
-- +goose Up
CREATE SCHEMA crm;
GRANT USAGE ON SCHEMA crm TO seguros_app;

CREATE TABLE crm.leads (
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL,
    name          text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 200),
    email         text CHECK (char_length(email) <= 254),
    phone         text CHECK (char_length(phone) <= 30),
    source        text NOT NULL CHECK (source IN ('referral', 'social', 'website', 'partner', 'cross_sell', 'call', 'event', 'other')),
    line_of_business_id uuid,
    agent_id      uuid,
    party_id      uuid,
    status        text NOT NULL CHECK (status IN ('new', 'contacted', 'interested', 'quoting', 'won', 'lost', 'follow_up')),
    lost_reason   text CHECK (char_length(lost_reason) <= 500),
    follow_up_on  date,
    contacted_at  timestamptz,
    assigned_at   timestamptz,
    created_at    timestamptz NOT NULL,
    created_by    uuid,
    updated_at    timestamptz NOT NULL,
    updated_by    uuid,
    version       integer NOT NULL CHECK (version > 0),
    CONSTRAINT leads_contact_check CHECK (email IS NOT NULL OR phone IS NOT NULL),
    CONSTRAINT leads_lost_check CHECK (status <> 'lost' OR lost_reason IS NOT NULL),
    CONSTRAINT leads_follow_up_check CHECK (status <> 'follow_up' OR follow_up_on IS NOT NULL),
    CONSTRAINT leads_party_check CHECK (status NOT IN ('quoting', 'won') OR party_id IS NOT NULL)
);
CREATE INDEX leads_tenant_keyset_idx ON crm.leads (tenant_id, id DESC);
CREATE INDEX leads_agent_idx ON crm.leads (tenant_id, agent_id, id DESC);

ALTER TABLE crm.leads ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.leads FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.leads USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON crm.leads TO seguros_app;

-- +goose Down
DROP TABLE crm.leads;
DROP SCHEMA crm;
