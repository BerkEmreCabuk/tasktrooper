-- Three unrelated per-repository facts that all live on the same row, added
-- together because they are one release:
--
--   mobile_platform    which platform a mobile repo targets (ios / android /
--                      cross_platform, '' = unset). Detected from the working
--                      copy at import and correctable by hand. Sub-projects
--                      carry their own inside sub_projects (migration 118) —
--                      that column is read and written whole, so it needs no
--                      schema change.
--   docs_task_id       the board task of the last reference-doc bundle: one
--                      task, one branch, one pull request holding every
--                      requested doc. '' = none outstanding.
--   mutation_enabled   the mutation-score twin of require_overall_coverage /
--   mutation_threshold coverage_threshold: whether the score is judged against
--                      a bar, and the bar. 0 means no number was set.
--
-- TEXT rather than an enum for mobile_platform, and NOT NULL DEFAULT '' rather
-- than nullable, for the same reason kind is: the domain validates the value
-- before it is ever persisted, and '' is a real state ("nobody has said"),
-- which a NULL would only spell a second way.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS mobile_platform    TEXT             NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS docs_task_id       TEXT             NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS mutation_enabled   BOOLEAN          NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS mutation_threshold DOUBLE PRECISION NOT NULL DEFAULT 0;
