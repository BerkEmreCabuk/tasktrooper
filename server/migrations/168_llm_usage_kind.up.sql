-- llm_usage was written ONLY by the RecordingClient wrapping the HTTP LLM
-- client, so every row today is either a chat/completion call or an Embed
-- call (Embed records an estimated prompt count, zero completion, model "").
-- Host CLI agent sessions (Claude Code, cursor-agent, opencode, antigravity)
-- never reached this table at all: those executors only added their usage to
-- the per-run context accumulator that ends up on task_agent_runs, so an
-- install that runs mostly CLI agents saw a usage dashboard that was
-- essentially 100% embedding noise. kind/provider let a row say what it
-- actually paid for; the backfill below recovers CLI history from
-- task_agent_runs so the dashboard isn't empty for everyone upgrading.
ALTER TABLE llm_usage ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'api';
ALTER TABLE llm_usage ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT '';

ALTER TABLE llm_usage DROP CONSTRAINT IF EXISTS llm_usage_kind_check;
ALTER TABLE llm_usage ADD CONSTRAINT llm_usage_kind_check CHECK (kind IN ('api', 'cli', 'embedding'));

-- A single CLI session's cumulative prompt (including cache reads across a
-- long-running task) can run into the millions; task_agent_runs' own token
-- columns are already BIGINT for the same reason.
ALTER TABLE llm_usage ALTER COLUMN prompt_tokens TYPE BIGINT;
ALTER TABLE llm_usage ALTER COLUMN completion_tokens TYPE BIGINT;

-- Every existing row was recorded by RecordingClient before kind existed.
-- Embed is the only path that records zero completion AND zero cache
-- activity (a real generation call always has completion tokens), so that
-- exact shape reclassifies cleanly without touching a single chat row.
UPDATE llm_usage
SET kind = 'embedding'
WHERE kind = 'api'
  AND completion_tokens = 0
  AND cache_read_tokens = 0
  AND cache_write_tokens = 0;

-- Backfill CLI history from task_agent_runs, one llm_usage row per run, so the
-- dashboard has something to show for installs that only ever ran host CLI
-- agents. Skipped when the run's own window already has an 'api'-kind row:
-- a CLI run's counters also include any HTTP calls RecordingClient already
-- ledgered for that run (e.g. a summarizer call), and double-counting those
-- would overstate spend for exactly the runs that mixed both paths.
INSERT INTO llm_usage (kind, provider, model, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, created_at)
SELECT 'cli', a.provider_type, '(unrecorded)',
       r.prompt_tokens, r.completion_tokens, r.cache_read_tokens, r.cache_write_tokens, r.updated_at
FROM task_agent_runs r
JOIN agents a ON a.id = r.agent_id
WHERE (r.prompt_tokens > 0 OR r.completion_tokens > 0 OR r.cache_read_tokens > 0 OR r.cache_write_tokens > 0)
  AND a.provider_type IN ('claude_code', 'cursor_agent', 'antigravity', 'opencode')
  AND NOT EXISTS (
      SELECT 1 FROM llm_usage u
      WHERE u.kind = 'api'
        AND u.created_at BETWEEN r.created_at AND r.updated_at
  );
