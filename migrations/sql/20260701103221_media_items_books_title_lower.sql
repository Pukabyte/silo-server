-- +goose NO TRANSACTION

-- +goose Up
-- literaryworks.ListMatchCandidates matches ebooks against audiobooks (and vice
-- versa) by exact case-insensitive title. Combined with the provider-id and
-- series EXISTS predicates it forms an OR, so the planner can only bitmap-OR the
-- whole filter through indexes when every branch is indexable; without a
-- LOWER(title) index the title branch forces a full media_items scan and negates
-- the provider/series indexes. Scoped to books so it stays small on large
-- movie/show catalogs.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_items_books_title_lower
    ON media_items (LOWER(title))
    WHERE type IN ('ebook', 'audiobook');

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_media_items_books_title_lower;
