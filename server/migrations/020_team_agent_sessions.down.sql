DROP INDEX IF EXISTS idx_sessions_team_agent;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_team_agent_pair;
ALTER TABLE sessions DROP COLUMN IF EXISTS agent_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS team_id;
