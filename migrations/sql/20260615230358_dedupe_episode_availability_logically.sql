-- +goose Up
-- episode_availability records "this logical episode has already been
-- available in this library." The original primary key only covered the
-- catalog episode_id, so a series re-ID or episode-row re-mint (e.g. a local
-- series gaining a provider match) could remap that id and make an
-- already-present episode look newly available. Collapse any existing
-- duplicates and enforce the logical identity going forward.
WITH ranked AS (
    SELECT
        library_id,
        episode_id,
        row_number() OVER (
            PARTITION BY library_id, series_id, episode_key
            ORDER BY available_at ASC, created_at ASC, episode_id ASC
        ) AS rn
    FROM public.episode_availability
)
DELETE FROM public.episode_availability ea
USING ranked r
WHERE ea.library_id = r.library_id
  AND ea.episode_id = r.episode_id
  AND r.rn > 1;

CREATE UNIQUE INDEX episode_availability_logical_episode_key
    ON public.episode_availability (library_id, series_id, episode_key);

-- The non-unique (library_id, series_id, episode_key DESC) index is now
-- redundant: the unique index above covers the same leading columns (Postgres
-- scans it backwards for DESC reads) and the table is insert-only, so nothing
-- reads it. Drop it rather than carry two indexes over the same tuple.
DROP INDEX IF EXISTS public.episode_availability_series_idx;

-- +goose Down
CREATE INDEX episode_availability_series_idx
    ON public.episode_availability (library_id, series_id, episode_key DESC);
DROP INDEX IF EXISTS public.episode_availability_logical_episode_key;
