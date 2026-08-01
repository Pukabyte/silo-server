-- +goose Up
ALTER TABLE audiobook_series ADD COLUMN series_key TEXT NOT NULL DEFAULT '';
ALTER TABLE ebook_series ADD COLUMN series_key TEXT NOT NULL DEFAULT '';

UPDATE audiobook_series
SET series_key = lower(regexp_replace(btrim(series_name), '[^[:alnum:]]', '', 'g'));
UPDATE ebook_series
SET series_key = lower(regexp_replace(btrim(series_name), '[^[:alnum:]]', '', 'g'));

DELETE FROM audiobook_series WHERE series_key = '';
DELETE FROM ebook_series WHERE series_key = '';

ALTER TABLE audiobook_series
    ADD CONSTRAINT audiobook_series_key_not_empty CHECK (series_key <> '');
ALTER TABLE ebook_series
    ADD CONSTRAINT ebook_series_key_not_empty CHECK (series_key <> '');

CREATE INDEX audiobook_series_key_index
    ON audiobook_series (series_key, series_index NULLS LAST);
CREATE INDEX ebook_series_key_index
    ON ebook_series (series_key, series_index NULLS LAST);

-- +goose Down
DROP INDEX IF EXISTS ebook_series_key_index;
DROP INDEX IF EXISTS audiobook_series_key_index;
ALTER TABLE ebook_series DROP CONSTRAINT IF EXISTS ebook_series_key_not_empty;
ALTER TABLE audiobook_series DROP CONSTRAINT IF EXISTS audiobook_series_key_not_empty;
ALTER TABLE ebook_series DROP COLUMN IF EXISTS series_key;
ALTER TABLE audiobook_series DROP COLUMN IF EXISTS series_key;
