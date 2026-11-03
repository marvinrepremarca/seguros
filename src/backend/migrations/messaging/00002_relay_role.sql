-- Auditoría de seguridad 1.8 (A1): las funciones del relay cruzan tenants, así que solo las
-- ejecuta el rol del worker (seguros_worker); la API (seguros_app) conserva únicamente el INSERT
-- en el outbox para su propio tenant.
-- También (B4): un mensaje que falla al publicarse no bloquea la cola: se reclaman primero los de
-- menos intentos y, tras 25 intentos, deja de reclamarse (queda para revisión manual).
-- +goose Up
REVOKE EXECUTE ON FUNCTION messaging.claim_outbox(integer) FROM seguros_app;
REVOKE EXECUTE ON FUNCTION messaging.mark_published(uuid[], timestamptz) FROM seguros_app;
REVOKE EXECUTE ON FUNCTION messaging.mark_failed(uuid, text) FROM seguros_app;
REVOKE EXECUTE ON FUNCTION messaging.purge_published(timestamptz) FROM seguros_app;
GRANT EXECUTE ON FUNCTION messaging.claim_outbox(integer) TO seguros_worker;
GRANT EXECUTE ON FUNCTION messaging.mark_published(uuid[], timestamptz) TO seguros_worker;
GRANT EXECUTE ON FUNCTION messaging.mark_failed(uuid, text) TO seguros_worker;
GRANT EXECUTE ON FUNCTION messaging.purge_published(timestamptz) TO seguros_worker;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION messaging.claim_outbox(p_limit integer)
RETURNS TABLE (id uuid, tenant_id uuid, topic text, payload jsonb, occurred_at timestamptz, attempts integer)
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT o.id, o.tenant_id, o.topic, o.payload, o.occurred_at, o.attempts
    FROM messaging.outbox o
    WHERE o.published_at IS NULL AND o.attempts < 25
    ORDER BY o.attempts, o.occurred_at, o.id
    LIMIT least(greatest(p_limit, 1), 500)
    FOR UPDATE SKIP LOCKED
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION messaging.claim_outbox(p_limit integer)
RETURNS TABLE (id uuid, tenant_id uuid, topic text, payload jsonb, occurred_at timestamptz, attempts integer)
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT o.id, o.tenant_id, o.topic, o.payload, o.occurred_at, o.attempts
    FROM messaging.outbox o
    WHERE o.published_at IS NULL
    ORDER BY o.occurred_at, o.id
    LIMIT least(greatest(p_limit, 1), 500)
    FOR UPDATE SKIP LOCKED
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION messaging.claim_outbox(integer) FROM seguros_worker;
REVOKE EXECUTE ON FUNCTION messaging.mark_published(uuid[], timestamptz) FROM seguros_worker;
REVOKE EXECUTE ON FUNCTION messaging.mark_failed(uuid, text) FROM seguros_worker;
REVOKE EXECUTE ON FUNCTION messaging.purge_published(timestamptz) FROM seguros_worker;
GRANT EXECUTE ON FUNCTION messaging.claim_outbox(integer) TO seguros_app;
GRANT EXECUTE ON FUNCTION messaging.mark_published(uuid[], timestamptz) TO seguros_app;
GRANT EXECUTE ON FUNCTION messaging.mark_failed(uuid, text) TO seguros_app;
GRANT EXECUTE ON FUNCTION messaging.purge_published(timestamptz) TO seguros_app;
