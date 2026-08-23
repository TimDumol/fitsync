CREATE TABLE IF NOT EXISTS public.fitsync_items (
    user_id text NOT NULL DEFAULT auth.user_id(),
    item_type text NOT NULL CHECK (item_type IN ('routine', 'draft', 'workout', 'settings')),
    item_id text NOT NULL,
    payload jsonb,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    PRIMARY KEY (user_id, item_type, item_id),
    CHECK ((deleted_at IS NULL AND payload IS NOT NULL) OR deleted_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS fitsync_items_user_updated_idx
    ON public.fitsync_items (user_id, updated_at DESC);

ALTER TABLE public.fitsync_items ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS fitsync_items_select_own ON public.fitsync_items;
CREATE POLICY fitsync_items_select_own ON public.fitsync_items
    FOR SELECT TO authenticated
    USING ((SELECT auth.user_id()) = user_id);

DROP POLICY IF EXISTS fitsync_items_insert_own ON public.fitsync_items;
CREATE POLICY fitsync_items_insert_own ON public.fitsync_items
    FOR INSERT TO authenticated
    WITH CHECK ((SELECT auth.user_id()) = user_id);

DROP POLICY IF EXISTS fitsync_items_update_own ON public.fitsync_items;
CREATE POLICY fitsync_items_update_own ON public.fitsync_items
    FOR UPDATE TO authenticated
    USING ((SELECT auth.user_id()) = user_id)
    WITH CHECK ((SELECT auth.user_id()) = user_id);

DROP POLICY IF EXISTS fitsync_items_delete_own ON public.fitsync_items;
CREATE POLICY fitsync_items_delete_own ON public.fitsync_items
    FOR DELETE TO authenticated
    USING ((SELECT auth.user_id()) = user_id);

GRANT SELECT, INSERT, UPDATE, DELETE ON public.fitsync_items TO authenticated;
