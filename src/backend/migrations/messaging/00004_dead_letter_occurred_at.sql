-- El reproceso conserva la fecha original del evento, comprueba el tenant al republicar y el rol de
-- plataforma ya no puede leer el contenido de los mensajes.
-- +goose Up
ALTER TABLE messaging.dead_letters ADD COLUMN occurred_at timestamptz;
REVOKE SELECT ON messaging.dead_letters FROM seguros_platform;
GRANT SELECT (id, tenant_id, consumer, topic, event_id, failed_at, retried_at) ON messaging.dead_letters TO seguros_platform;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION messaging.retry_dead_letter(p_tenant uuid, p_id uuid, p_at timestamptz)
RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE d messaging.dead_letters%ROWTYPE;
BEGIN
    UPDATE messaging.dead_letters SET retried_at = p_at
    WHERE id = p_id AND tenant_id = p_tenant AND retried_at IS NULL
    RETURNING * INTO d;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    INSERT INTO messaging.outbox AS o (id, tenant_id, topic, payload, occurred_at)
    VALUES (d.event_id, d.tenant_id, d.topic, d.payload, coalesce(d.occurred_at, d.failed_at))
    ON CONFLICT (id) DO UPDATE SET published_at = NULL, attempts = 0, last_error = NULL
    WHERE o.tenant_id = EXCLUDED.tenant_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'evento de otro tenant';
    END IF;
    RETURN true;
END;
$$;
-- +goose StatementEnd

-- +goose Down
REVOKE SELECT (id, tenant_id, consumer, topic, event_id, failed_at, retried_at) ON messaging.dead_letters FROM seguros_platform;
GRANT SELECT ON messaging.dead_letters TO seguros_platform;
ALTER TABLE messaging.dead_letters DROP COLUMN occurred_at;
