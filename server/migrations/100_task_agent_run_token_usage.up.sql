-- Per-run token counters, same motivation as 071's tool counters: a run's
-- token spend existed only in llm_usage's global per-call rows, which carry no
-- task or agent identity, so the task detail could never answer "what did THIS
-- agent burn on THIS task". The runner sums every LLM call the run made (loop
-- turns, summarizer, orchestrator subtasks) and stamps the totals here.
--
-- Same contract as llm_usage / domain.Usage: prompt_tokens is the TOTAL prompt
-- size, cache_read/cache_write are subsets of it, not additions.
ALTER TABLE task_agent_runs
    ADD COLUMN IF NOT EXISTS llm_calls          INT    NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS prompt_tokens      BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS completion_tokens  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_tokens  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_write_tokens BIGINT NOT NULL DEFAULT 0;
