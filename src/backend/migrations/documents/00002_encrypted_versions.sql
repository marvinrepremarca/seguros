-- Los archivos sensibles se guardan cifrados por la aplicación (hallazgo de auditoría de la Fase 5).
-- +goose Up
ALTER TABLE documents.versions ADD COLUMN encrypted boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE documents.versions DROP COLUMN encrypted;
