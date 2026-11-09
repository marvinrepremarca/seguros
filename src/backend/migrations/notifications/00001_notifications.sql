-- Notificaciones (Flujos 4, 6 y 7): bandeja de salida. El mensaje se redacta al enviarlo.
-- +goose Up
CREATE SCHEMA notifications;
GRANT USAGE ON SCHEMA notifications TO seguros_app;

CREATE TABLE notifications.messages (
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL,
    template    text NOT NULL CHECK (char_length(template) BETWEEN 2 AND 60),
    channel     text NOT NULL CHECK (channel IN ('email')),
    policy_id   uuid NOT NULL,
    party_id    uuid,
    params      jsonb NOT NULL,
    dedupe_key  text NOT NULL CHECK (char_length(dedupe_key) <= 200),
    status      text NOT NULL CHECK (status IN ('pending', 'sent', 'failed', 'skipped')),
    skip_reason text CHECK (skip_reason IN ('no_email', 'consent_revoked', 'no_recipient')),
    attempts    integer NOT NULL CHECK (attempts >= 0),
    sent_at     timestamptz,
    created_at  timestamptz NOT NULL,
    created_by  uuid,
    updated_at  timestamptz NOT NULL,
    updated_by  uuid,
    version     integer NOT NULL CHECK (version > 0),
    CONSTRAINT messages_dedupe_key UNIQUE (tenant_id, dedupe_key),
    CONSTRAINT messages_params_size CHECK (pg_column_size(params) <= 2048)
);
CREATE INDEX messages_tenant_keyset_idx ON notifications.messages (tenant_id, id DESC);
CREATE INDEX messages_pending_idx ON notifications.messages (tenant_id, id) WHERE status = 'pending';

ALTER TABLE notifications.messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications.messages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notifications.messages USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON notifications.messages TO seguros_app;

-- +goose StatementBegin
CREATE FUNCTION notifications.tenants_with_pending_messages()
RETURNS TABLE (tenant_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT DISTINCT m.tenant_id FROM notifications.messages m WHERE m.status = 'pending'
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION notifications.tenants_with_pending_messages() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION notifications.tenants_with_pending_messages() TO seguros_worker;

-- +goose Down
DROP FUNCTION notifications.tenants_with_pending_messages();
DROP TABLE notifications.messages;
DROP SCHEMA notifications;
