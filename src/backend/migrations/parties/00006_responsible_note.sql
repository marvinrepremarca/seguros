-- DECISIONS N12: la nota opcional del traspaso individual; el motivo pasa a ser un código del catálogo.
-- +goose Up
ALTER TABLE parties.party_responsible_history ADD COLUMN note text CHECK (char_length(note) <= 200);

-- +goose Down
ALTER TABLE parties.party_responsible_history DROP COLUMN note;
