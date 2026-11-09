-- Reclamo de envíos (evita correos duplicados entre jobs) y espera entre intentos.
-- +goose Up
ALTER TABLE notifications.messages ADD COLUMN next_attempt_at timestamptz;

-- +goose Down
ALTER TABLE notifications.messages DROP COLUMN next_attempt_at;
