-- A gate's approval is only reusable across a bounce (merge conflict rebase,
-- a pre-existing CI failure, "no changes needed") when the diff it approved
-- is provably the same one, not just the same commit SHA — a rebase changes
-- every SHA on the branch without changing the patch. verified_patch_id
-- mirrors verified_sha (migration 139) per criterion check; patch_id on
-- task_column_spans records the diff a code_review approval closed its span
-- on, keyed by board_column so a later revisit gets its own value.
ALTER TABLE task_criterion_checks ADD COLUMN IF NOT EXISTS verified_patch_id TEXT NOT NULL DEFAULT '';
ALTER TABLE task_column_spans ADD COLUMN IF NOT EXISTS patch_id TEXT NOT NULL DEFAULT '';
