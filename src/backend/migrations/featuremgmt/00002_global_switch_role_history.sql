-- Auditoría de seguridad 1.7 (A1, M4): los switches globales afectan a todos los tenants, así que
-- solo los escribe el rol de cmd/platformctl (seguros_platform); la API y el worker solo los leen.
-- Cada cambio queda en un historial append-only con operador, valor anterior/nuevo y motivo, en la
-- misma transacción (audit.events es por tenant y estos cambios no tienen uno).
-- +goose Up
REVOKE INSERT, UPDATE, DELETE ON featuremgmt.global_switches FROM seguros_app;
GRANT INSERT, UPDATE, DELETE ON featuremgmt.global_switches TO seguros_platform;

ALTER TABLE featuremgmt.global_switches
    ADD COLUMN updated_by text CHECK (char_length(updated_by) BETWEEN 1 AND 120);

CREATE TABLE featuremgmt.global_switch_history (
    id         uuid        PRIMARY KEY,
    flag_key   text        NOT NULL CHECK (char_length(flag_key) <= 120),
    previous   boolean,
    new        boolean,
    reason     text        NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 500),
    operator   text        NOT NULL CHECK (char_length(operator) BETWEEN 1 AND 120),
    changed_at timestamptz NOT NULL
);
CREATE INDEX global_switch_history_flag_idx ON featuremgmt.global_switch_history (flag_key, changed_at);
GRANT SELECT, INSERT ON featuremgmt.global_switch_history TO seguros_platform;

-- +goose StatementBegin
CREATE FUNCTION featuremgmt.forbid_history_mutation() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'global_switch_history es append-only' USING ERRCODE = 'insufficient_privilege';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER global_switch_history_append_only BEFORE UPDATE OR DELETE ON featuremgmt.global_switch_history
    FOR EACH ROW EXECUTE FUNCTION featuremgmt.forbid_history_mutation();

-- +goose Down
DROP TABLE featuremgmt.global_switch_history;
DROP FUNCTION featuremgmt.forbid_history_mutation();
ALTER TABLE featuremgmt.global_switches DROP COLUMN updated_by;
REVOKE INSERT, UPDATE, DELETE ON featuremgmt.global_switches FROM seguros_platform;
GRANT INSERT, UPDATE, DELETE ON featuremgmt.global_switches TO seguros_app;
