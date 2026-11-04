-- Auditoría de N10 (B3): policy_scopes define a qué clientes alcanza un asesor. Solo la escribe el
-- consumidor del worker; la API solo la lee, así un fallo en la API no puede forjar alcance.
-- +goose Up
REVOKE INSERT, UPDATE ON parties.policy_scopes FROM seguros_app;
GRANT INSERT, UPDATE ON parties.policy_scopes TO seguros_worker;

-- +goose Down
REVOKE INSERT, UPDATE ON parties.policy_scopes FROM seguros_worker;
GRANT INSERT, UPDATE ON parties.policy_scopes TO seguros_app;
