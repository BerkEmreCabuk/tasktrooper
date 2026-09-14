-- Per-agent limits for the claude_code CLI executor.
--
-- Both default to the "unset" value the domain already uses for Model: 0 and
-- '' mean "the executor's own default", so every existing agent keeps the
-- behaviour it had before this column existed.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS max_turns INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS effort TEXT NOT NULL DEFAULT '';
