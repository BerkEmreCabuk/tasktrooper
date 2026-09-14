ALTER TABLE agent_performance_scores ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
ALTER TABLE agent_performance_scores DROP CONSTRAINT IF EXISTS agent_performance_scores_agent_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_scores_agent_team
    ON agent_performance_scores(agent_id, team_id) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_scores_agent_legacy
    ON agent_performance_scores(agent_id) WHERE team_id IS NULL;

ALTER TABLE agent_score_events ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;

UPDATE agent_score_events e
SET team_id = r.team_id
FROM board_tasks t
JOIN repositories r ON r.id = t.repository_id
WHERE e.task_id = t.id AND e.team_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_agent_score_events_agent_team
    ON agent_score_events(agent_id, team_id, created_at DESC);

INSERT INTO agent_performance_scores (agent_id, team_id, score, runs_total, runs_passed, runs_revised)
SELECT e.agent_id,
       e.team_id,
       GREATEST(0, 100.0 + SUM(e.delta)),
       COUNT(*),
       COUNT(*) FILTER (WHERE e.delta > 0),
       COUNT(*) FILTER (WHERE e.delta < 0)
FROM agent_score_events e
WHERE e.team_id IS NOT NULL
GROUP BY e.agent_id, e.team_id
ON CONFLICT DO NOTHING;
