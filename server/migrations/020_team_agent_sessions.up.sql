ALTER TABLE sessions ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS agent_id UUID REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_team_agent_pair;
ALTER TABLE sessions ADD CONSTRAINT sessions_team_agent_pair
    CHECK ((team_id IS NULL AND agent_id IS NULL) OR (team_id IS NOT NULL AND agent_id IS NOT NULL));

CREATE INDEX IF NOT EXISTS idx_sessions_team_agent ON sessions(team_id, agent_id, updated_at DESC);
