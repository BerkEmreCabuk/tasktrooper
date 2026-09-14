DROP TABLE IF EXISTS agent_score_events;
DROP TABLE IF EXISTS agent_performance_scores;
ALTER TABLE team_agent_column_subscriptions DROP COLUMN IF EXISTS task_type_filter;
