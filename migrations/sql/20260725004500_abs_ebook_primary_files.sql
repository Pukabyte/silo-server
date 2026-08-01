-- Persist the primary ebook selected through the Audiobookshelf compatibility
-- API. A missing row intentionally means the ABS default: prefer EPUB, then
-- the first supported ebook file.
-- +goose Up
CREATE TABLE IF NOT EXISTS abs_ebook_primary_files (
    content_id TEXT PRIMARY KEY,
    file_id INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS abs_ebook_primary_files;
