ALTER TABLE agents ADD COLUMN IF NOT EXISTS self_evolution_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE agent_templates ADD COLUMN IF NOT EXISTS self_evolution_enabled BOOLEAN NOT NULL DEFAULT false;
