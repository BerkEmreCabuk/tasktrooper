-- The store identity a mobile working copy already states about itself, read
-- at import next to mobile_platform (migration 123) and stored so it can be
-- served from a host that does not hold the code:
--
--   detected_bundle_id      iOS PRODUCT_BUNDLE_IDENTIFIER / CFBundleIdentifier
--   detected_package_name   Android applicationId / manifest package
--
-- '' = nobody could read one. DETECTED, not chosen: what a repository actually
-- ships under is a deploy target's bundle_id / package_name var, and these two
-- only prefill that form so nobody retypes what build.gradle already says.
--
-- Persisted rather than read on demand because the two are not read on the
-- same machine: detection runs where the working copy is, while the deploy
-- config endpoint is served by a shared agent-server whose disk may hold no
-- copy of this repository at all.
--
-- Sub-projects carry their own inside sub_projects (migration 118) — that
-- column is read and written whole, so it needs no schema change.
--
-- TEXT NOT NULL DEFAULT '' for the same reason mobile_platform is: '' is a
-- real state, which a NULL would only spell a second way.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS detected_bundle_id    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS detected_package_name TEXT NOT NULL DEFAULT '';
