-- Fork-only: the devops role and the purpose that routes infrastructure work
-- (deploy setup, incident tasks) to it. The devops-engineer agent itself is
-- created by the catalog sync (catalog/agents/devops-engineer), which assigns
-- it this role. With nobody holding the role the purpose resolves nobody, and
-- deploy/prodops fall back to system_task_assignee as before.
INSERT INTO roles (key, name, description, required_tools) VALUES
    ('devops', 'DevOps Engineer', 'Owns pipelines, images, manifests, deploy setup and the incident loop.', '{}')
ON CONFLICT (key) DO NOTHING;

ALTER TABLE role_purposes DROP CONSTRAINT IF EXISTS role_purposes_purpose_check;
ALTER TABLE role_purposes ADD CONSTRAINT role_purposes_purpose_check
    CHECK (purpose IN ('system_task_assignee', 'repo_profiler', 'infra_task_assignee'));

INSERT INTO role_purposes (purpose, role_id)
VALUES ('infra_task_assignee', (SELECT id FROM roles WHERE key = 'devops'))
ON CONFLICT (purpose) DO NOTHING;
