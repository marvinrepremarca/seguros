-- Una revalidación de sesión detectó que el usuario salió de la organización del tenant: deja de
-- ofrecerse como asesor activo. El siguiente login en esa organización lo limpia.
-- +goose Up
ALTER TABLE identity.users ADD COLUMN left_organization_at timestamptz;

-- +goose Down
ALTER TABLE identity.users DROP COLUMN left_organization_at;
