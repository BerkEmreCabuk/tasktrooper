DROP INDEX IF EXISTS idx_rules_agent_id;
DROP INDEX IF EXISTS idx_skills_agent_id;
DROP INDEX IF EXISTS idx_rules_agent_name;
DROP INDEX IF EXISTS idx_skills_agent_name;

CREATE TABLE IF NOT EXISTS agent_skills (
    agent_id  UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    skill_id  UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, skill_id)
);

INSERT INTO agent_skills (agent_id, skill_id)
SELECT agent_id, id FROM skills ON CONFLICT DO NOTHING;

ALTER TABLE skills DROP COLUMN IF EXISTS agent_id;
ALTER TABLE orchestrator_rules DROP COLUMN IF EXISTS agent_id;

ALTER TABLE skills ADD CONSTRAINT skills_name_key UNIQUE (name);
ALTER TABLE orchestrator_rules ADD CONSTRAINT orchestrator_rules_name_key UNIQUE (name);
