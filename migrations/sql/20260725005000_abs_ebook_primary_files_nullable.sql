-- ABS status toggles the current primary file into the supplementary set.
-- NULL records that explicit no-primary state; no row still means scanner default.
-- +goose Up
ALTER TABLE abs_ebook_primary_files ALTER COLUMN file_id DROP NOT NULL;

-- +goose Down
DELETE FROM abs_ebook_primary_files WHERE file_id IS NULL;
ALTER TABLE abs_ebook_primary_files ALTER COLUMN file_id SET NOT NULL;
