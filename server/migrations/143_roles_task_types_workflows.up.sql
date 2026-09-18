-- Roles, task types and per-type workflow stages as data (release B).
--
-- Code must not know which agent is which role, nor branch on task types or
-- mid-lifecycle columns; see server/.ai/architecture.md and
-- internal/domain/{role,workflow,workflow_behaviour}.go for the model this
-- schema backs. This migration only writes data that reproduces TODAY's
-- board behaviour — see internal/application/workflow/workflowtest for the
-- Go builder the parity test compares these rows against, and the three
-- deliberate differences called out below (technical skips pm_uat).

-- ---------------------------------------------------------------------------
-- Schema
-- ---------------------------------------------------------------------------

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key text NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_]*$'),
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    required_tools text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agent_role_assignments (
    role_id uuid NOT NULL REFERENCES roles ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES agents ON DELETE CASCADE,
    -- NULL = any area.
    areas text[] NULL CHECK (areas IS NULL OR areas <@ ARRAY['backend','frontend','mobile']::text[]),
    priority int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, agent_id)
);

CREATE TABLE role_purposes (
    purpose text PRIMARY KEY CHECK (purpose IN ('system_task_assignee','repo_profiler')),
    role_id uuid NULL REFERENCES roles ON DELETE SET NULL
);

CREATE TABLE task_types (
    key text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]*$'),
    label text NOT NULL,
    key_prefix text NOT NULL UNIQUE CHECK (key_prefix ~ '^[A-Z]{1,4}$'),
    position int NOT NULL DEFAULT 0,
    is_default bool NOT NULL DEFAULT false,
    is_defect bool NOT NULL DEFAULT false,
    assignee_role_id uuid NULL REFERENCES roles ON DELETE SET NULL,
    assignee_mode text NOT NULL DEFAULT 'none' CHECK (assignee_mode IN ('none','default','override')),
    behaviours jsonb NOT NULL DEFAULT '[]',
    built_in bool NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX task_types_one_default ON task_types (is_default) WHERE is_default;

CREATE TABLE workflow_stages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_type text NOT NULL REFERENCES task_types(key) ON DELETE CASCADE,
    -- Deliberately no FK to board_columns: ReplaceColumns (postgres/board_config.go)
    -- DELETEs every board_columns row and reinserts on every save, so an FK
    -- here would fail that write. application/workspace.Service.UpdateColumns
    -- refuses instead, when a removed slug still has a stage with behaviours.
    column_slug text NOT NULL,
    position int NOT NULL,
    on_path bool NOT NULL DEFAULT true,
    kind text NOT NULL CHECK (kind IN ('intake','queue','work','review','approval','rework','parked','terminal')),
    -- e.g. [{"key":"advance_on_diff","params":{"to":"code_review"}}]
    behaviours jsonb NOT NULL DEFAULT '[]',
    instructions text NOT NULL DEFAULT '',
    UNIQUE (task_type, column_slug)
);

CREATE TABLE workflow_stage_participants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    stage_id uuid NOT NULL REFERENCES workflow_stages ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles ON DELETE CASCADE,
    mode text NOT NULL CHECK (mode IN ('worker','approver')),
    instructions text NOT NULL DEFAULT '',
    position int NOT NULL DEFAULT 0,
    UNIQUE (stage_id, role_id)
);

ALTER TABLE agent_templates
    ADD COLUMN roles jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN subscriptions jsonb NOT NULL DEFAULT '[]';

-- ---------------------------------------------------------------------------
-- Roles and their built-in assignments (by exact agent name — an install
-- whose operator renamed a built-in agent gets no assignment for it, the same
-- "nil" today's hardcoded name comparisons would already answer with; see
-- release-b-plan.md §5 "Renamed agents get no role assignment").
-- ---------------------------------------------------------------------------

INSERT INTO roles (key, name, description, required_tools) VALUES
    ('developer', 'Developer', 'Writes the implementation for task/bug/technical work.', '{}'),
    -- Literal snapshot of domain.RequiredAnalizTools at the time this
    -- migration was written (CodeExplorationTools ++ AnalizDocumentTools ++
    -- BoardProgressTools ++ four extras) — see tool_policy.go and
    -- workflowtest.requiredAnalizToolsSnapshot, which the migration test
    -- checks this against.
    ('analyst', 'Analyst', 'Produces the spec and implementation plan for an analiz task.',
     ARRAY['codebase_search','grep_code','get_repo_tree','get_symbol_skeleton','expand_symbol_context','read_file',
           'add_task_document','update_task_document','claim_board_task','move_board_task',
           'add_task_comment','list_task_comments','list_task_documents','create_board_task']),
    ('architect', 'Architect', 'Reviews diffs and designs rather than writing code.', '{}'),
    ('qa', 'QA', 'Tests the product as a black box before UAT.', '{}'),
    ('product_manager', 'Product Manager', 'Verifies acceptance criteria against QA''s evidence.', '{}');

INSERT INTO agent_role_assignments (role_id, agent_id, areas, priority)
SELECT (SELECT id FROM roles WHERE key = 'developer'), a.id,
       CASE a.name WHEN 'backend-developer' THEN ARRAY['backend'] WHEN 'frontend-developer' THEN ARRAY['frontend'] ELSE ARRAY['mobile'] END,
       0
FROM agents a
WHERE a.name IN ('backend-developer', 'frontend-developer', 'mobile-developer');

INSERT INTO agent_role_assignments (role_id, agent_id, areas, priority)
SELECT (SELECT id FROM roles WHERE key = 'architect'), a.id, NULL, 0
FROM agents a WHERE a.name = 'system-architect';

INSERT INTO agent_role_assignments (role_id, agent_id, areas, priority)
SELECT (SELECT id FROM roles WHERE key = 'qa'), a.id, NULL, 0
FROM agents a WHERE a.name = 'qa-agent';

INSERT INTO agent_role_assignments (role_id, agent_id, areas, priority)
SELECT (SELECT id FROM roles WHERE key = 'product_manager'), a.id, NULL, 0
FROM agents a WHERE a.name = 'product-manager';

INSERT INTO role_purposes (purpose, role_id) VALUES
    ('system_task_assignee', (SELECT id FROM roles WHERE key = 'developer')),
    ('repo_profiler', (SELECT id FROM roles WHERE key = 'architect'));

-- The analyst role's assignments come from the per-area analiz_assignee_*
-- settings (domain.AnalizAssigneeForArea's own default: an unset/blank area
-- means system-architect). Agents are grouped by which areas resolve to them;
-- an agent covering all three collapses to one areas=NULL (any area) row,
-- matching domain.RoleResolver.AgentForRole's own "specific area beats any
-- area" reading. A named agent this install has no row for (renamed/deleted)
-- is silently skipped, same as every other role assignment above.
DO $$
DECLARE
    backend_agent  text;
    frontend_agent text;
    mobile_agent   text;
    analyst_role   uuid := (SELECT id FROM roles WHERE key = 'analyst');
BEGIN
    backend_agent  := COALESCE(NULLIF((SELECT value FROM app_settings WHERE key = 'analiz_assignee_backend'), ''), 'system-architect');
    frontend_agent := COALESCE(NULLIF((SELECT value FROM app_settings WHERE key = 'analiz_assignee_frontend'), ''), 'system-architect');
    mobile_agent   := COALESCE(NULLIF((SELECT value FROM app_settings WHERE key = 'analiz_assignee_mobile'), ''), 'system-architect');

    CREATE TEMP TABLE _analiz_area_map (agent_name text, area text) ON COMMIT DROP;
    INSERT INTO _analiz_area_map (agent_name, area) VALUES
        (backend_agent, 'backend'), (frontend_agent, 'frontend'), (mobile_agent, 'mobile');

    INSERT INTO agent_role_assignments (role_id, agent_id, areas, priority)
    SELECT analyst_role, a.id,
           CASE WHEN COUNT(*) = 3 THEN NULL ELSE array_agg(m.area ORDER BY m.area) END,
           0
    FROM _analiz_area_map m
    JOIN agents a ON a.name = m.agent_name
    GROUP BY a.id
    ON CONFLICT (role_id, agent_id) DO NOTHING;
END $$;

DELETE FROM app_settings WHERE key LIKE 'analiz_assignee_%';

-- ---------------------------------------------------------------------------
-- Task types
-- ---------------------------------------------------------------------------

INSERT INTO task_types (key, label, key_prefix, position, is_default, is_defect, assignee_role_id, assignee_mode, behaviours, built_in) VALUES
    ('task', 'Task', 'T', 0, true, false, NULL, 'none', '[]', true),
    ('analiz', 'Analysis', 'A', 1, false, false, (SELECT id FROM roles WHERE key = 'analyst'), 'override',
        '[{"key":"document_deliverable"},{"key":"no_workspace_writes"},{"key":"require_repo_grounding"}]', true),
    ('bug', 'Bug', 'B', 2, false, true, NULL, 'none', '[]', true),
    ('technical', 'Technical', 'TC', 3, false, false, NULL, 'none', '[]', true);

-- board_tasks.task_type is now governed by this table rather than a static
-- CHECK list — a custom type an operator adds is a legal task_type the moment
-- it exists here, with no further migration.
ALTER TABLE board_tasks DROP CONSTRAINT board_tasks_task_type_check;
ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_task_type_fk
    FOREIGN KEY (task_type) REFERENCES task_types(key);

-- ---------------------------------------------------------------------------
-- Workflow stages: every type gets all 13 default columns. Position reads
-- board_columns.position when the board already has that slug (an existing
-- install), else falls back to this listed order (a fresh install, whose
-- board_columns the bootseed step has not written yet at migration time).
--
-- The behaviour sets below were verified cell-by-cell against the code they
-- replace (dispatcher.go, review.go, runner.go, review_sweep.go,
-- tool_policy.go — see release-b-plan.md §0/§1/§3) and reproduce today's
-- behaviour exactly, with three deliberate exceptions carrying out migration
-- 140's intent for `technical`: its ready_for_qa/in_qa review-verdict sweep
-- passes to human_uat instead of pm_uat, its pm_uat stage carries no
-- review_chain_stage (its review chain does not require pm_uat), and its
-- pm_uat stage is off the on-path spine. Task keys additionally now render
-- from this table's key_prefix (TC-n for technical) rather than a hardcoded
-- Go switch that had no 'technical' case (see postgres/repository.go).
-- ---------------------------------------------------------------------------

-- backlog (intake), position fallback 0
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'backlog', COALESCE((SELECT position FROM board_columns WHERE slug = 'backlog'), 0), true, 'intake',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('bug',       'backlog', COALESCE((SELECT position FROM board_columns WHERE slug = 'backlog'), 0), true, 'intake',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('technical', 'backlog', COALESCE((SELECT position FROM board_columns WHERE slug = 'backlog'), 0), true, 'intake',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('analiz',    'backlog', COALESCE((SELECT position FROM board_columns WHERE slug = 'backlog'), 0), true, 'intake',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"}]', '');

-- todo (queue), position fallback 1
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'todo', COALESCE((SELECT position FROM board_columns WHERE slug = 'todo'), 1), true, 'queue',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"block_on_dependencies"},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('bug',       'todo', COALESCE((SELECT position FROM board_columns WHERE slug = 'todo'), 1), true, 'queue',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"block_on_dependencies"},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('technical', 'todo', COALESCE((SELECT position FROM board_columns WHERE slug = 'todo'), 1), true, 'queue',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"block_on_dependencies"},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('analiz',    'todo', COALESCE((SELECT position FROM board_columns WHERE slug = 'todo'), 1), true, 'queue',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"block_on_dependencies"},{"key":"criteria_sweep"},{"key":"commit_on_finish"}]',
    'This is an ANALIZ task (task_type=analiz) in `todo` — an ANALYSIS, not an implementation. If it is not relevant to your role, take no action. If it is: claim it and move it to in_progress as the opening action of the step that does the analysis (never a step of its own), then investigate in this same run — clone/pull every repository the task names, read the relevant code, and decide WHAT is needed and WHERE. Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — a document attached by a run that explored nothing is rejected and the run is failed. If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer''s job on the wrong task. Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — the human approves there, and no implementation task is created before they do.');

-- in_progress (work), position fallback 2
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'in_progress', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_progress'), 2), true, 'work',
    '[{"key":"block_on_dependencies","params":{"refuse_move":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"advance_on_diff","params":{"to":"code_review"}}]', ''),
('bug',       'in_progress', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_progress'), 2), true, 'work',
    '[{"key":"block_on_dependencies","params":{"refuse_move":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"advance_on_diff","params":{"to":"code_review"}}]', ''),
('technical', 'in_progress', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_progress'), 2), true, 'work',
    '[{"key":"block_on_dependencies","params":{"refuse_move":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"advance_on_diff","params":{"to":"code_review"}}]', ''),
('analiz',    'in_progress', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_progress'), 2), true, 'work',
    '[{"key":"block_on_dependencies","params":{"refuse_move":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"advance_on_document","params":{"to":"analiz_review"}}]',
    'This is an ANALIZ task (task_type=analiz) ALREADY claimed and ALREADY in `in_progress` — an ANALYSIS, not an implementation, and the move you might be tempted to plan first has happened. Continue the investigation from where it stands and finish it in this run. Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — a document attached by a run that explored nothing is rejected and the run is failed. If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer''s job on the wrong task. Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — the human approves there, and no implementation task is created before they do.');

-- analiz_review (approval), position fallback 3
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'analiz_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'analiz_review'), 3), false, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"review_only"},{"key":"strip_writers"}]', ''),
('bug',       'analiz_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'analiz_review'), 3), false, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"review_only"},{"key":"strip_writers"}]', ''),
('technical', 'analiz_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'analiz_review'), 3), false, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"review_only"},{"key":"strip_writers"}]', ''),
('analiz',    'analiz_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'analiz_review'), 3), true, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"review_only"},{"key":"strip_writers"},{"key":"review_chain_stage","params":{"label":"analiz review","remedy":"move it to analiz_review and approve the spec/plan there"}}]',
    'This is an ANALIZ task (task_type=analiz) in `analiz_review`: it is waiting on a HUMAN to approve or reject the spec/plan. Nothing is yours to do here — do not move it, do not rewrite the documents, and do not create implementation tasks. Take no action.');

-- code_review (review), position fallback 4
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'code_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'code_review'), 4), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"wait_for_ci"},{"key":"ensure_pr_on_enter"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"per_step"}},{"key":"hold_for_human_approval"},{"key":"review_only"},{"key":"require_pr_for_review"},{"key":"require_criteria_complete"},{"key":"strip_writers"},{"key":"review_verdict_sweep","params":{"pass_to":"ready_for_qa"}},{"key":"show_all_criteria"},{"key":"review_chain_stage","params":{"label":"code review","remedy":"move it to code_review so the diff is reviewed"}}]', ''),
('bug',       'code_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'code_review'), 4), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"wait_for_ci"},{"key":"ensure_pr_on_enter"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"per_step"}},{"key":"hold_for_human_approval"},{"key":"review_only"},{"key":"require_pr_for_review"},{"key":"require_criteria_complete"},{"key":"strip_writers"},{"key":"review_verdict_sweep","params":{"pass_to":"ready_for_qa"}},{"key":"show_all_criteria"},{"key":"review_chain_stage","params":{"label":"code review","remedy":"move it to code_review so the diff is reviewed"}}]', ''),
('technical', 'code_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'code_review'), 4), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"wait_for_ci"},{"key":"ensure_pr_on_enter"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"per_step"}},{"key":"hold_for_human_approval"},{"key":"review_only"},{"key":"require_pr_for_review"},{"key":"require_criteria_complete"},{"key":"strip_writers"},{"key":"review_verdict_sweep","params":{"pass_to":"ready_for_qa"}},{"key":"show_all_criteria"},{"key":"review_chain_stage","params":{"label":"code review","remedy":"move it to code_review so the diff is reviewed"}}]', ''),
('analiz',    'code_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'code_review'), 4), false, 'review',
    '[{"key":"route_to_subscribers"},{"key":"wait_for_ci"},{"key":"ensure_pr_on_enter"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"per_step"}},{"key":"hold_for_human_approval"},{"key":"review_only"},{"key":"require_pr_for_review"},{"key":"require_criteria_complete"},{"key":"strip_writers"}]', '');

-- ready_for_qa (queue), position fallback 5
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'ready_for_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'ready_for_qa'), 5), true, 'queue',
    '[{"key":"route_to_subscribers"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"qa"}},{"key":"require_criteria_complete"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"auto_enter","params":{"to":"in_qa","assignee_only":"false"}},{"key":"require_execution_evidence"},{"key":"show_all_criteria"},{"key":"review_verdict_sweep","params":{"pass_to":"pm_uat"}}]', ''),
('bug',       'ready_for_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'ready_for_qa'), 5), true, 'queue',
    '[{"key":"route_to_subscribers"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"qa"}},{"key":"require_criteria_complete"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"auto_enter","params":{"to":"in_qa","assignee_only":"false"}},{"key":"require_execution_evidence"},{"key":"show_all_criteria"},{"key":"review_verdict_sweep","params":{"pass_to":"pm_uat"}}]', ''),
('technical', 'ready_for_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'ready_for_qa'), 5), true, 'queue',
    '[{"key":"route_to_subscribers"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"qa"}},{"key":"require_criteria_complete"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"auto_enter","params":{"to":"in_qa","assignee_only":"false"}},{"key":"require_execution_evidence"},{"key":"show_all_criteria"},{"key":"review_verdict_sweep","params":{"pass_to":"human_uat"}}]', ''),
('analiz',    'ready_for_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'ready_for_qa'), 5), false, 'queue',
    '[{"key":"route_to_subscribers"},{"key":"detect_migration_on_enter"},{"key":"stage_deploy_on_enter","params":{"when":"qa"}},{"key":"require_criteria_complete"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"}]', '');

-- in_qa (review), position fallback 6
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'in_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_qa'), 6), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"require_execution_evidence"},{"key":"show_all_criteria"},{"key":"review_verdict_sweep","params":{"pass_to":"pm_uat"}},{"key":"review_chain_stage","params":{"label":"QA","remedy":"move it to ready_for_qa; QA takes it into in_qa and tests it there"}}]', ''),
('bug',       'in_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_qa'), 6), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"require_execution_evidence"},{"key":"show_all_criteria"},{"key":"review_verdict_sweep","params":{"pass_to":"pm_uat"}},{"key":"review_chain_stage","params":{"label":"QA","remedy":"move it to ready_for_qa; QA takes it into in_qa and tests it there"}}]', ''),
('technical', 'in_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_qa'), 6), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"require_execution_evidence"},{"key":"show_all_criteria"},{"key":"review_verdict_sweep","params":{"pass_to":"human_uat"}},{"key":"review_chain_stage","params":{"label":"QA","remedy":"move it to ready_for_qa; QA takes it into in_qa and tests it there"}}]', ''),
('analiz',    'in_qa', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_qa'), 6), false, 'review',
    '[{"key":"route_to_subscribers"},{"key":"criterion_verdict","params":{"channel":"qa"}},{"key":"require_test_cases"},{"key":"strip_writers"},{"key":"no_read_file"},{"key":"commit_on_finish"}]', '');

-- need_revision (rework), position fallback 7 — off-path for every type
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'need_revision', COALESCE((SELECT position FROM board_columns WHERE slug = 'need_revision'), 7), false, 'rework',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"advance_on_diff","params":{"to":"code_review"}}]', ''),
('bug',       'need_revision', COALESCE((SELECT position FROM board_columns WHERE slug = 'need_revision'), 7), false, 'rework',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"advance_on_diff","params":{"to":"code_review"}}]', ''),
('technical', 'need_revision', COALESCE((SELECT position FROM board_columns WHERE slug = 'need_revision'), 7), false, 'rework',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"advance_on_diff","params":{"to":"code_review"}}]', ''),
('analiz',    'need_revision', COALESCE((SELECT position FROM board_columns WHERE slug = 'need_revision'), 7), false, 'rework',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"criteria_sweep"},{"key":"commit_on_finish"},{"key":"advance_on_document","params":{"to":"analiz_review"}}]',
    'This is an ANALIZ task (task_type=analiz) in `need_revision`: the human rejected the analysis. Their comment is in the task comments in your context. Revise the spec/plan at the ROOT of the concern — re-read the code where you are unsure — and attach the corrected documents. Create no implementation task from a rejected analysis. Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — a document attached by a run that explored nothing is rejected and the run is failed. If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer''s job on the wrong task. Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — the human approves there, and no implementation task is created before they do.');

-- pm_uat (review), position fallback 8
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'pm_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'pm_uat'), 8), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"criterion_verdict","params":{"channel":"pm"}},{"key":"require_product_check"},{"key":"review_only"},{"key":"strip_writers"},{"key":"no_code_reading"},{"key":"review_verdict_sweep","params":{"pass_to":"human_uat"}},{"key":"show_all_criteria"},{"key":"review_chain_stage","params":{"label":"UAT","remedy":"move it to pm_uat so every acceptance criterion is verified against QA''s evidence"}}]', ''),
('bug',       'pm_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'pm_uat'), 8), true, 'review',
    '[{"key":"route_to_subscribers"},{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"criterion_verdict","params":{"channel":"pm"}},{"key":"require_product_check"},{"key":"review_only"},{"key":"strip_writers"},{"key":"no_code_reading"},{"key":"review_verdict_sweep","params":{"pass_to":"human_uat"}},{"key":"show_all_criteria"},{"key":"review_chain_stage","params":{"label":"UAT","remedy":"move it to pm_uat so every acceptance criterion is verified against QA''s evidence"}}]', ''),
-- technical: no review_chain_stage — migration 140's intent, its review
-- chain does not require pm_uat (see ReviewChainForType today).
('technical', 'pm_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'pm_uat'), 8), false, 'review',
    '[{"key":"route_to_subscribers"},{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"criterion_verdict","params":{"channel":"pm"}},{"key":"require_product_check"},{"key":"review_only"},{"key":"strip_writers"},{"key":"no_code_reading"},{"key":"review_verdict_sweep","params":{"pass_to":"human_uat"}},{"key":"show_all_criteria"}]', ''),
('analiz',    'pm_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'pm_uat'), 8), false, 'review',
    '[{"key":"route_to_subscribers"},{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"criterion_verdict","params":{"channel":"pm"}},{"key":"require_product_check"},{"key":"review_only"},{"key":"strip_writers"},{"key":"no_code_reading"}]', '');

-- human_uat (approval), position fallback 9
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'human_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'human_uat'), 9), true, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"forward_exit"},{"key":"strip_writers"},{"key":"no_code_reading"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"show_all_criteria"}]', ''),
('bug',       'human_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'human_uat'), 9), true, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"forward_exit"},{"key":"strip_writers"},{"key":"no_code_reading"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"show_all_criteria"}]', ''),
('technical', 'human_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'human_uat'), 9), true, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"forward_exit"},{"key":"strip_writers"},{"key":"no_code_reading"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"show_all_criteria"}]', ''),
('analiz',    'human_uat', COALESCE((SELECT position FROM board_columns WHERE slug = 'human_uat'), 9), false, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"forward_exit"},{"key":"strip_writers"},{"key":"no_code_reading"},{"key":"commit_on_finish"}]', '');

-- blocked (parked), position fallback 10 — off-path for every type
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'blocked', COALESCE((SELECT position FROM board_columns WHERE slug = 'blocked'), 10), false, 'parked',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('bug',       'blocked', COALESCE((SELECT position FROM board_columns WHERE slug = 'blocked'), 10), false, 'parked',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('technical', 'blocked', COALESCE((SELECT position FROM board_columns WHERE slug = 'blocked'), 10), false, 'parked',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"},{"key":"build_verify"}]', ''),
('analiz',    'blocked', COALESCE((SELECT position FROM board_columns WHERE slug = 'blocked'), 10), false, 'parked',
    '[{"key":"dispatch_suspended"},{"key":"commit_on_finish"}]', '');

-- done (terminal), position fallback 11
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'done', COALESCE((SELECT position FROM board_columns WHERE slug = 'done'), 11), true, 'terminal',
    '[{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"strip_writers","params":{"allow":"merge_task_pull_request,release_control"}},{"key":"dispatch_suspended"},{"key":"merge_pr_on_enter"},{"key":"watch_deploy_on_resume"}]', ''),
('bug',       'done', COALESCE((SELECT position FROM board_columns WHERE slug = 'done'), 11), true, 'terminal',
    '[{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"strip_writers","params":{"allow":"merge_task_pull_request,release_control"}},{"key":"dispatch_suspended"},{"key":"merge_pr_on_enter"},{"key":"watch_deploy_on_resume"}]', ''),
('technical', 'done', COALESCE((SELECT position FROM board_columns WHERE slug = 'done'), 11), true, 'terminal',
    '[{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"strip_writers","params":{"allow":"merge_task_pull_request,release_control"}},{"key":"dispatch_suspended"},{"key":"merge_pr_on_enter"},{"key":"watch_deploy_on_resume"}]', ''),
('analiz',    'done', COALESCE((SELECT position FROM board_columns WHERE slug = 'done'), 11), true, 'terminal',
    '[{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"strip_writers","params":{"allow":"merge_task_pull_request,release_control"}}]',
    'This is an ANALIZ task (task_type=analiz) the human moved to `done` — that move IS the approval of your spec and plan. Now decompose it: one implementation task per repository and per layer, each with its own plan slice, testable acceptance criteria and an assignee (call list_team for the roster; order them by dependency — backend API before the frontend/mobile that consumes it). Write no code yourself. List the created tasks in a comment and move this analiz task to `released` as the last action of the step that created them.');

-- released (terminal), position fallback 12
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('task',      'released', COALESCE((SELECT position FROM board_columns WHERE slug = 'released'), 12), true, 'terminal',
    '[{"key":"dispatch_suspended"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"watch_deploy_on_resume"},{"key":"require_release_deploy"}]', ''),
('bug',       'released', COALESCE((SELECT position FROM board_columns WHERE slug = 'released'), 12), true, 'terminal',
    '[{"key":"dispatch_suspended"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"watch_deploy_on_resume"},{"key":"require_release_deploy"}]', ''),
('technical', 'released', COALESCE((SELECT position FROM board_columns WHERE slug = 'released'), 12), true, 'terminal',
    '[{"key":"dispatch_suspended"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"commit_on_finish"},{"key":"build_verify"},{"key":"watch_deploy_on_resume"},{"key":"require_release_deploy"}]', ''),
('analiz',    'released', COALESCE((SELECT position FROM board_columns WHERE slug = 'released'), 12), true, 'terminal',
    '[{"key":"dispatch_suspended"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"enforce_review_chain"},{"key":"commit_on_finish"}]', '');

-- ---------------------------------------------------------------------------
-- Participants (informational in B — not read for dispatch until WP-C).
-- ---------------------------------------------------------------------------

INSERT INTO workflow_stage_participants (stage_id, role_id, mode, position)
SELECT ws.id, (SELECT id FROM roles WHERE key = 'developer'), 'worker', 0
FROM workflow_stages ws
WHERE ws.task_type IN ('task','bug','technical') AND ws.column_slug IN ('in_progress','need_revision');

INSERT INTO workflow_stage_participants (stage_id, role_id, mode, position)
SELECT ws.id, (SELECT id FROM roles WHERE key = 'architect'), 'approver', 0
FROM workflow_stages ws
WHERE ws.task_type IN ('task','bug','technical') AND ws.column_slug = 'code_review';

INSERT INTO workflow_stage_participants (stage_id, role_id, mode, position)
SELECT ws.id, (SELECT id FROM roles WHERE key = 'qa'), 'worker', 0
FROM workflow_stages ws
WHERE ws.task_type IN ('task','bug','technical') AND ws.column_slug IN ('ready_for_qa','in_qa','done','released');

INSERT INTO workflow_stage_participants (stage_id, role_id, mode, position)
SELECT ws.id, (SELECT id FROM roles WHERE key = 'product_manager'), 'approver', 0
FROM workflow_stages ws
WHERE ws.task_type IN ('task','bug','technical') AND ws.column_slug = 'pm_uat';

INSERT INTO workflow_stage_participants (stage_id, role_id, mode, position)
SELECT ws.id, (SELECT id FROM roles WHERE key = 'analyst'), 'worker', 0
FROM workflow_stages ws
WHERE ws.task_type = 'analiz' AND ws.column_slug IN ('in_progress','need_revision','done');
