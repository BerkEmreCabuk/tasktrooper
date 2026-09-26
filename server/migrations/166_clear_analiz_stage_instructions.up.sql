-- The behavioural prompt migration 143 seeded into workflow_stages.instructions
-- for analiz's todo/in_progress/need_revision/analiz_review/done now lives in
-- catalog/agents/system-architect's column md files instead (the agent that
-- actually runs those stages), synced into agent_column_instructions the same
-- way every other role's column prompt is. Clearing instructions back to ''
-- here restores columnInstruction's normal per-column fallback for analiz,
-- matching every other task type. Comparing the exact stored text (rather
-- than blanket-clearing every analiz row) means a stage an operator already
-- edited through the HTTP API keeps its own text.
UPDATE workflow_stages
SET instructions = ''
WHERE task_type = 'analiz'
  AND column_slug = 'todo'
  AND instructions = 'This is an ANALIZ task (task_type=analiz) in `todo` — an ANALYSIS, not an implementation. If it is not relevant to your role, take no action. If it is: claim it and move it to in_progress as the opening action of the step that does the analysis (never a step of its own), then investigate in this same run — clone/pull every repository the task names, read the relevant code, and decide WHAT is needed and WHERE. Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — a document attached by a run that explored nothing is rejected and the run is failed. If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer''s job on the wrong task. Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — the human approves there, and no implementation task is created before they do.';

UPDATE workflow_stages
SET instructions = ''
WHERE task_type = 'analiz'
  AND column_slug = 'in_progress'
  AND instructions = 'This is an ANALIZ task (task_type=analiz) ALREADY claimed and ALREADY in `in_progress` — an ANALYSIS, not an implementation, and the move you might be tempted to plan first has happened. Continue the investigation from where it stands and finish it in this run. Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — a document attached by a run that explored nothing is rejected and the run is failed. If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer''s job on the wrong task. Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — the human approves there, and no implementation task is created before they do.';

UPDATE workflow_stages
SET instructions = ''
WHERE task_type = 'analiz'
  AND column_slug = 'analiz_review'
  AND instructions = 'This is an ANALIZ task (task_type=analiz) in `analiz_review`: it is waiting on a HUMAN to approve or reject the spec/plan. Nothing is yours to do here — do not move it, do not rewrite the documents, and do not create implementation tasks. Take no action.';

UPDATE workflow_stages
SET instructions = ''
WHERE task_type = 'analiz'
  AND column_slug = 'need_revision'
  AND instructions = 'This is an ANALIZ task (task_type=analiz) in `need_revision`: the human rejected the analysis. Their comment is in the task comments in your context. Revise the spec/plan at the ROOT of the concern — re-read the code where you are unsure — and attach the corrected documents. Create no implementation task from a rejected analysis. Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — a document attached by a run that explored nothing is rejected and the run is failed. If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer''s job on the wrong task. Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — the human approves there, and no implementation task is created before they do.';

UPDATE workflow_stages
SET instructions = ''
WHERE task_type = 'analiz'
  AND column_slug = 'done'
  AND instructions = 'This is an ANALIZ task (task_type=analiz) the human moved to `done` — that move IS the approval of your spec and plan. Now decompose it: one implementation task per repository and per layer, each with its own plan slice, testable acceptance criteria and an assignee (call list_team for the roster; order them by dependency — backend API before the frontend/mobile that consumes it). Write no code yourself. List the created tasks in a comment and move this analiz task to `released` as the last action of the step that created them.';
