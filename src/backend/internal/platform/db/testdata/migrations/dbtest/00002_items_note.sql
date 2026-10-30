-- +goose Up
ALTER TABLE dbtest.items ADD COLUMN note text;

-- +goose Down
ALTER TABLE dbtest.items DROP COLUMN note;
