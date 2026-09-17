-- A fourth relation type: discovered_from.
--
-- Beads (an AI-agent issue tracker) records a discovered-from link when an
-- agent, mid-task, opens a task for something it ran into but was not asked
-- to do. TaskTrooper's create_board_task now does the same automatically:
-- inside a task run, the task it opens gets a discovered_from row pointing at
-- the task the run was working on.
--
-- It is not derived_from wearing a different label. derived_from is the spec
-- route — repository.Service.AnalysisReferences reads ONLY derived_from and
-- feeds the target task's documents into the new task's run context, because
-- an analiz task's deliverable IS its documents and nothing else points an
-- implementation task at them. A task discovered while working another task
-- has no such deliverable to hand over; recording it as derived_from would
-- inject documents that were never written for it. discovered_from is
-- provenance only — it orders nothing, gates nothing, injects nothing into
-- any run. Reusing derived_from's read path for it would be the bug this
-- migration exists to avoid.
--
-- Additive: a widened CHECK, nothing else. No existing row changes meaning.

-- Migration 106 widened this constraint from ('blocks', 'deploy_depends_on')
-- to add 'derived_from'; the same drop-and-recreate widens it again. Keeping
-- the constraint is the point — see migration 106's comment on why a typo'd
-- relation_type must never become a silently unmatched row.
ALTER TABLE task_relations DROP CONSTRAINT IF EXISTS task_relations_relation_type_check;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_relation_type_check
    CHECK (relation_type IN ('blocks', 'deploy_depends_on', 'derived_from', 'discovered_from'));
