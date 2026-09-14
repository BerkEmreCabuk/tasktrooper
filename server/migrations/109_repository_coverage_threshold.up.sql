-- Two settings that decide what coverage is allowed to block.
--
-- Before this, the gate was whole-repo coverage against a hard 90. That is the
-- wrong number to block on: a codebase measuring 44% cannot reach 90 with the
-- change in front of it, so every task was refused entry to code_review — which
-- is where the pull request is opened, so no PR could be opened at all. The
-- override that was supposed to relieve this (domain.Repository.CoverageThreshold,
-- honoured by board.coverageThreshold) had a struct field, a branch and a doc
-- comment, but never a column: every read produced the zero value and fell
-- through to the default. The escape hatch could not be opened from anywhere.
--
-- The blocking number moves to NEW-CODE coverage (the lines this task's diff
-- added or changed), which a change is always able to satisfy no matter what
-- the surrounding repo measures. Whole-repo coverage stays measured and
-- reported, and only blocks when an owner opts in here.
--
-- 0 keeps meaning "use the default": the override is opt-in, and existing rows
-- must not silently acquire a threshold nobody chose.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS coverage_threshold DOUBLE PRECISION NOT NULL DEFAULT 0;

-- Off by default, and deliberately so. Arming it retroactively on every
-- existing repository would reproduce the deadlock this migration exists to
-- end. The owner turns it on when their repo can clear the bar.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS require_overall_coverage BOOLEAN NOT NULL DEFAULT false;
