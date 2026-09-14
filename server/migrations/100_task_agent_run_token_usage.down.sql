ALTER TABLE task_agent_runs
    DROP COLUMN IF EXISTS llm_calls,
    DROP COLUMN IF EXISTS prompt_tokens,
    DROP COLUMN IF EXISTS completion_tokens,
    DROP COLUMN IF EXISTS cache_read_tokens,
    DROP COLUMN IF EXISTS cache_write_tokens;
