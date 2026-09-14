-- An agent's skills split into two kinds, and until now the schema could only
-- express one of them. A skill is either GENERAL — it holds whatever the code
-- is written in — or it only means anything inside one technology: "write a
-- repository layer" is a different skill in Go than it is in Django, and an
-- agent that carries both applies the wrong one half the time.
--
-- tech_stack_id NULL is the general skill; a set one names the single stack it
-- belongs to. A nullable column rather than a join table because the relation
-- IS at most one: a skill written for Django is not also a Flutter skill, and a
-- join table would let it claim to be.
--
-- The stack list is per AGENT, not global: a stack is one agent's way of
-- organising its own skills, and two agents that both say "Go" are describing
-- their own catalogs, not sharing a row.
--
-- set_config, and it is load-bearing: ALTER TABLE ... ADD CONSTRAINT below
-- revalidates existing rows, `skills` carries FORCE ROW LEVEL SECURITY, and a
-- migration is schema-wide with no tenant in it — current_setting would throw
-- 42704 the instant Postgres evaluated the first row's policy. Same reasoning
-- and same nil uuid as migration 121; SET LOCAL semantics, so it dies with this
-- transaction.
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000000', true);

CREATE TABLE IF NOT EXISTS agent_tech_stacks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid,
    agent_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_tech_stacks_agent_id_fkey
        FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT agent_tech_stacks_tenant_key UNIQUE (tenant_id, id),
    CONSTRAINT agent_tech_stacks_agent_name_key UNIQUE (tenant_id, agent_id, name)
);

ALTER TABLE agent_tech_stacks ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_tech_stacks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_tech_stacks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE INDEX IF NOT EXISTS idx_agent_tech_stacks_agent
    ON agent_tech_stacks(tenant_id, agent_id, position, name);

ALTER TABLE skills ADD COLUMN IF NOT EXISTS tech_stack_id UUID;

-- ON DELETE SET NULL names its column: on a COMPOSITE key the unqualified form
-- nulls every column in the tuple, tenant_id included, and skills.tenant_id is
-- NOT NULL — deleting a stack would crash with 23502 instead of turning its
-- skills back into general ones. Migration 121 exists because twelve
-- constraints got this wrong.
ALTER TABLE skills ADD CONSTRAINT skills_tech_stack_id_fkey
    FOREIGN KEY (tenant_id, tech_stack_id) REFERENCES agent_tech_stacks(tenant_id, id)
    ON DELETE SET NULL (tech_stack_id);

CREATE INDEX IF NOT EXISTS idx_skills_tech_stack
    ON skills(tenant_id, tech_stack_id) WHERE tech_stack_id IS NOT NULL;
