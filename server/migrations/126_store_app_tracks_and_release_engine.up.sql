-- Two additions to the mobile release surface, added together because they
-- are one release:
--
--   mobile_store_apps.app_name          the store console's own display name
--                                        for the app, written the moment it is
--                                        chosen (from ListApps, or the single
--                                        app an account has). '' = not chosen
--                                        yet.
--   mobile_store_apps.tracks            a CACHE of domain.StoreTracks — the
--                                        three channels (internal / external /
--                                        production) as ASC / Play last
--                                        reported them. The store console
--                                        stays the source of truth; this
--                                        column only lets the release panel
--                                        render without a live round-trip.
--   mobile_store_apps.tracks_synced_at  when that cache was last refreshed.
--                                        Nullable rather than defaulted at row
--                                        creation, because "never synced" is a
--                                        real state distinct from "synced
--                                        once, long ago" — and an empty
--                                        '{}'::jsonb tracks cannot tell the
--                                        two apart on its own.
--
--   repositories.release_engine         where THIS repository's mobile
--                                        releases are built and uploaded (see
--                                        domain.ReleaseEngine*). It sits on
--                                        the repository, not on a deploy
--                                        target or environment, because the
--                                        two engines — GitHub Actions and a
--                                        paired local runner — build the same
--                                        script against the same working copy
--                                        into the same artifact: the choice is
--                                        about which machine is available to
--                                        do the building, not about what gets
--                                        shipped.
--
-- app_name and release_engine are TEXT NOT NULL DEFAULT '' / 'auto' rather
-- than nullable, for the same reason every other column in this family is:
-- the domain validates the value before it is ever persisted, and an unset
-- one is a real state which a NULL would only spell a second way.
ALTER TABLE mobile_store_apps
    ADD COLUMN IF NOT EXISTS app_name         TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tracks           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS tracks_synced_at TIMESTAMPTZ;

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS release_engine TEXT NOT NULL DEFAULT 'auto';
