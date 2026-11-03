-- Revisión de seguridad final de la Fase 1 (B-N1): crear particiones es tarea del job periódico,
-- que corre en cmd/worker; la API no necesita esta función.
-- +goose Up
REVOKE EXECUTE ON FUNCTION audit.ensure_partitions(date, integer) FROM seguros_app;
GRANT EXECUTE ON FUNCTION audit.ensure_partitions(date, integer) TO seguros_worker;

-- +goose Down
REVOKE EXECUTE ON FUNCTION audit.ensure_partitions(date, integer) FROM seguros_worker;
GRANT EXECUTE ON FUNCTION audit.ensure_partitions(date, integer) TO seguros_app;
