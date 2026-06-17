-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS public.audiobook_item_file_stats (
    media_folder_id integer NOT NULL,
    content_id text NOT NULL,
    duration_seconds bigint NOT NULL DEFAULT 0,
    active_file_count integer NOT NULL DEFAULT 0,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT audiobook_item_file_stats_pkey PRIMARY KEY (media_folder_id, content_id),
    CONSTRAINT audiobook_item_file_stats_media_folder_id_fkey FOREIGN KEY (media_folder_id) REFERENCES public.media_folders(id) ON DELETE CASCADE,
    CONSTRAINT audiobook_item_file_stats_content_id_fkey FOREIGN KEY (content_id) REFERENCES public.media_items(content_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_audiobook_item_file_stats_content
ON public.audiobook_item_file_stats USING btree (content_id, media_folder_id);

CREATE OR REPLACE FUNCTION public.refresh_audiobook_item_file_stats(p_media_folder_id integer, p_content_id text)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_item_type text;
    v_duration_seconds bigint;
    v_active_file_count integer;
BEGIN
    IF p_media_folder_id IS NULL OR NULLIF(BTRIM(p_content_id), '') IS NULL THEN
        RETURN;
    END IF;

    SELECT mi.type
    INTO v_item_type
    FROM public.media_items mi
    WHERE mi.content_id = p_content_id;

    IF v_item_type IS DISTINCT FROM 'audiobook' THEN
        DELETE FROM public.audiobook_item_file_stats
        WHERE media_folder_id = p_media_folder_id
          AND content_id = p_content_id;
        RETURN;
    END IF;

    SELECT
        COALESCE(SUM(COALESCE(mf.duration, 0)), 0)::bigint,
        COUNT(*)::integer
    INTO v_duration_seconds, v_active_file_count
    FROM public.media_files mf
    WHERE mf.media_folder_id = p_media_folder_id
      AND mf.content_id = p_content_id
      AND mf.missing_since IS NULL;

    IF COALESCE(v_active_file_count, 0) > 0 THEN
        INSERT INTO public.audiobook_item_file_stats (
            media_folder_id,
            content_id,
            duration_seconds,
            active_file_count,
            updated_at
        ) VALUES (
            p_media_folder_id,
            p_content_id,
            COALESCE(v_duration_seconds, 0),
            v_active_file_count,
            NOW()
        )
        ON CONFLICT (media_folder_id, content_id) DO UPDATE SET
            duration_seconds = EXCLUDED.duration_seconds,
            active_file_count = EXCLUDED.active_file_count,
            updated_at = NOW();
    ELSE
        DELETE FROM public.audiobook_item_file_stats
        WHERE media_folder_id = p_media_folder_id
          AND content_id = p_content_id;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION public.audiobook_item_file_stats_media_files_trigger()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM public.refresh_audiobook_item_file_stats(OLD.media_folder_id, OLD.content_id);
        RETURN OLD;
    END IF;

    IF TG_OP = 'UPDATE'
       AND (OLD.media_folder_id IS DISTINCT FROM NEW.media_folder_id OR OLD.content_id IS DISTINCT FROM NEW.content_id) THEN
        PERFORM public.refresh_audiobook_item_file_stats(OLD.media_folder_id, OLD.content_id);
    END IF;

    PERFORM public.refresh_audiobook_item_file_stats(NEW.media_folder_id, NEW.content_id);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION public.refresh_audiobook_item_file_stats_for_item(p_content_id text)
RETURNS void
LANGUAGE sql
AS $$
    SELECT public.refresh_audiobook_item_file_stats(active_files.media_folder_id, active_files.content_id)
    FROM (
        SELECT DISTINCT mf.media_folder_id, mf.content_id
        FROM public.media_files mf
        WHERE mf.content_id = p_content_id
    ) active_files;
$$;

CREATE OR REPLACE FUNCTION public.audiobook_item_file_stats_media_items_trigger()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM public.audiobook_item_file_stats
        WHERE content_id = OLD.content_id;
        RETURN OLD;
    END IF;

    IF TG_OP = 'UPDATE'
       AND (OLD.content_id IS DISTINCT FROM NEW.content_id OR OLD.type IS DISTINCT FROM NEW.type) THEN
        PERFORM public.refresh_audiobook_item_file_stats_for_item(OLD.content_id);
    END IF;

    PERFORM public.refresh_audiobook_item_file_stats_for_item(NEW.content_id);
    RETURN NEW;
END;
$$;

INSERT INTO public.audiobook_item_file_stats (
    media_folder_id,
    content_id,
    duration_seconds,
    active_file_count,
    updated_at
)
SELECT
    mf.media_folder_id,
    mf.content_id,
    COALESCE(SUM(COALESCE(mf.duration, 0)), 0)::bigint AS duration_seconds,
    COUNT(*)::integer AS active_file_count,
    NOW() AS updated_at
FROM public.media_files mf
JOIN public.media_items mi ON mi.content_id = mf.content_id
WHERE mi.type = 'audiobook'
  AND mf.content_id IS NOT NULL
  AND mf.missing_since IS NULL
GROUP BY mf.media_folder_id, mf.content_id
ON CONFLICT (media_folder_id, content_id) DO UPDATE SET
    duration_seconds = EXCLUDED.duration_seconds,
    active_file_count = EXCLUDED.active_file_count,
    updated_at = NOW();

DROP TRIGGER IF EXISTS trg_audiobook_item_file_stats_media_files_insert_delete ON public.media_files;
DROP TRIGGER IF EXISTS trg_audiobook_item_file_stats_media_files_update ON public.media_files;
DROP TRIGGER IF EXISTS trg_audiobook_item_file_stats_media_items ON public.media_items;

CREATE TRIGGER trg_audiobook_item_file_stats_media_files_insert_delete
AFTER INSERT OR DELETE ON public.media_files
FOR EACH ROW EXECUTE FUNCTION public.audiobook_item_file_stats_media_files_trigger();

CREATE TRIGGER trg_audiobook_item_file_stats_media_files_update
AFTER UPDATE ON public.media_files
FOR EACH ROW
WHEN (
    OLD.content_id IS DISTINCT FROM NEW.content_id OR
    OLD.media_folder_id IS DISTINCT FROM NEW.media_folder_id OR
    OLD.duration IS DISTINCT FROM NEW.duration OR
    OLD.missing_since IS DISTINCT FROM NEW.missing_since
)
EXECUTE FUNCTION public.audiobook_item_file_stats_media_files_trigger();

CREATE TRIGGER trg_audiobook_item_file_stats_media_items
AFTER INSERT OR UPDATE OF content_id, type OR DELETE ON public.media_items
FOR EACH ROW EXECUTE FUNCTION public.audiobook_item_file_stats_media_items_trigger();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_audiobook_item_file_stats_media_items ON public.media_items;
DROP TRIGGER IF EXISTS trg_audiobook_item_file_stats_media_files_update ON public.media_files;
DROP TRIGGER IF EXISTS trg_audiobook_item_file_stats_media_files_insert_delete ON public.media_files;

DROP FUNCTION IF EXISTS public.audiobook_item_file_stats_media_items_trigger();
DROP FUNCTION IF EXISTS public.refresh_audiobook_item_file_stats_for_item(text);
DROP FUNCTION IF EXISTS public.audiobook_item_file_stats_media_files_trigger();
DROP FUNCTION IF EXISTS public.refresh_audiobook_item_file_stats(integer, text);

DROP INDEX IF EXISTS public.idx_audiobook_item_file_stats_content;
DROP TABLE IF EXISTS public.audiobook_item_file_stats;
-- +goose StatementEnd
