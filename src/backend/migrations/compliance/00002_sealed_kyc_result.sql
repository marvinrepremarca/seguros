-- Resultado y fuente de una verificación SARLAFT son Restringidos: se guardan cifrados (auditoría de la
-- Fase 5). Las filas anteriores conservan el texto plano hasta una migración de contracción.
-- +goose Up
ALTER TABLE compliance.kyc_checks
    ADD COLUMN result_ciphertext bytea,
    ADD COLUMN source_ciphertext bytea,
    ALTER COLUMN result DROP NOT NULL,
    ALTER COLUMN source DROP NOT NULL,
    ADD CONSTRAINT kyc_checks_sealed_or_plain CHECK (
        (result IS NULL AND source IS NULL AND result_ciphertext IS NOT NULL AND source_ciphertext IS NOT NULL)
        OR (result IS NOT NULL AND source IS NOT NULL AND result_ciphertext IS NULL AND source_ciphertext IS NULL));

-- +goose Down
-- Solo revierte si no hay verificaciones cifradas: volver al texto plano exigiría descifrarlas.
ALTER TABLE compliance.kyc_checks
    DROP CONSTRAINT kyc_checks_sealed_or_plain,
    ALTER COLUMN result SET NOT NULL,
    ALTER COLUMN source SET NOT NULL,
    DROP COLUMN source_ciphertext,
    DROP COLUMN result_ciphertext;
