DELETE FROM agent_performance_scores WHERE team_id IS NOT NULL;
DROP INDEX IF EXISTS idx_agent_score_events_agent_team;
ALTER TABLE agent_score_events DROP COLUMN IF EXISTS team_id;
DROP INDEX IF EXISTS uq_agent_scores_agent_legacy;
DROP INDEX IF EXISTS uq_agent_scores_agent_team;
ALTER TABLE agent_performance_scores DROP COLUMN IF EXISTS team_id;
ALTER TABLE agent_performance_scores ADD CONSTRAINT agent_performance_scores_agent_id_key UNIQUE (agent_id);
