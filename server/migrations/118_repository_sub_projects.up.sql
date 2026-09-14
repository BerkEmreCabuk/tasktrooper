-- The addressable, human-curated list of a monorepo's sub-projects: path and
-- kind for each, detected at import (repository.DetectRepoSubProjects) and
-- then editable in the initial-setup dialog (retype a kind, remove a row).
--
-- Deliberately separate from sub_repo_kinds (migration 046): that column is a
-- deduplicated SET of kind values consumed by the GitHub Actions pipeline
-- routing feature (SavePipelineConfig) and by the async profile-proposal
-- pipeline (repoprofile) — neither of those is repurposed here, and this
-- column is not derived from nor derives that one.
--
-- JSONB, not a child table: the list is read and written whole (never queried
-- by path, never joined), and its shape is validated by
-- domain.ValidateSubProjects before it is ever persisted.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS sub_projects JSONB NOT NULL DEFAULT '[]'::jsonb;
