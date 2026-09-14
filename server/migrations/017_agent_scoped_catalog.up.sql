ALTER TABLE skills ADD COLUMN IF NOT EXISTS agent_id UUID REFERENCES agents(id) ON DELETE CASCADE;
ALTER TABLE orchestrator_rules ADD COLUMN IF NOT EXISTS agent_id UUID REFERENCES agents(id) ON DELETE CASCADE;

UPDATE skills s SET agent_id = j.agent_id
FROM agent_skills j WHERE s.id = j.skill_id AND s.agent_id IS NULL;

DELETE FROM skills WHERE agent_id IS NULL;
DELETE FROM orchestrator_rules WHERE agent_id IS NULL;

DELETE FROM agents WHERE name IN ('general-coder', 'shell-runner', 'code-explorer');

DROP TABLE IF EXISTS agent_skills;

ALTER TABLE skills ALTER COLUMN agent_id SET NOT NULL;
ALTER TABLE orchestrator_rules ALTER COLUMN agent_id SET NOT NULL;

ALTER TABLE skills DROP CONSTRAINT IF EXISTS skills_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_skills_agent_name ON skills(agent_id, name);

ALTER TABLE orchestrator_rules DROP CONSTRAINT IF EXISTS orchestrator_rules_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_rules_agent_name ON orchestrator_rules(agent_id, name);

CREATE INDEX IF NOT EXISTS idx_skills_agent_id ON skills(agent_id);
CREATE INDEX IF NOT EXISTS idx_rules_agent_id ON orchestrator_rules(agent_id);
