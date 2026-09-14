---
name: implementation-task-spec
category: pm
description: Use when you create an implementation board task directly - the required fields, one-role-one-deliverable rule, and dependency ordering
---

# Implementation Task Spec

## Overview

An implementation task is a self-contained unit of work for one developer role. The recurring failures are bundling two layers into one task and vague, untestable acceptance criteria.

**Core principle:** One task = one role = one deliverable.

## create_board_task fields

| Field | Value |
|-------|-------|
| type | `task` (or `bug` for defects) |
| column | `backlog` by default (stakeholder reviews before start); `todo` only if told to start immediately |
| assignee | one responsible developer role — REQUIRED: pass `assignee: "<role>"` (e.g. `backend-developer`) so that agent is dispatched. An unassigned task sits idle. Use `list_team` for valid names. |
| title | action-object ("Add task export endpoint", "Fix checkout price rounding") |
| description | PRODUCT only: user story ("As [persona], I want [capability], so that [outcome]") + context + out-of-scope + analiz reference if any |
| technical_description | TECHNICAL only: endpoints/files/schema touched, approach, constraints, interfaces consumed and produced. BEFORE filling it, verify every file and endpoint name against the actual repository: `get_repo_tree` for the layout, `codebase_search`/`grep_code` for the handler/route/symbol you are naming. Never write a path or endpoint you did not see — an invented path sends the developer to a file that does not exist. |
| acceptance_criteria | array of strings, one observable Given/When/Then per item, incl. error cases (see acceptance-criteria-gwt). About the PRODUCT only — never a board step ("moved to ready_for_qa", "PR opened", "QA notified"); those are dropped when the task is created |
| repository | which codebase the work touches — plain name (`"acme-web"`) or UUID. Resolve with `list_repositories`. Omitting it silently files the task against the default repository. |
| project | which initiative it belongs to — plain name (`"Acme"`) or UUID. Resolve with `list_projects`; `create_project` first if the initiative is new. |
| derived_from | the analiz task this work came out of, e.g. `["A-12"]`. Set it whenever an analysis exists. The spec and plan are DOCUMENTS on that analiz task — never a `docs/` commit — and this reference is what feeds them into the developer's run and lets them re-read the plan with `list_task_documents A-12`. |
| blocked_by | task keys that must be FINISHED before anyone starts this one, e.g. `["T-1"]`. Enforced: the board parks this task in `blocked` while any of them is open and picks it up automatically when the last one lands. |
| deploy_depends_on | task keys that must be LIVE IN PRODUCTION before this one may be released. Enforced at release time, and written into this task's `before_deploy` runbook automatically. |
| before_deploy / after_deploy / rollback_plan | what has to happen around the deploy — a migration to run first, a flag to flip after, how to undo it. Posted on the card automatically when the release is dispatched and when it lands. Put them here, not in a comment: a comment is not read at deploy time. |

## Rules

- **Verify names before writing them.** Everything in `technical_description` that names a file, endpoint, table or symbol comes from `get_repo_tree` + `codebase_search`/`grep_code` output, not from memory or plausibility. If you cannot find it, say what you looked for and leave the naming to the developer instead of guessing.
- **One field, one home.** `description`, `technical_description` and `acceptance_criteria` are three separate board fields, each rendered in its own section of the task. Never write acceptance criteria or technical detail into `description` — a pasted copy is what QA and pm_uat then read instead of the real checklist, and it silently goes stale when the real field is edited.
- **Criteria describe the product, not the board.** A column move, a PR, a comment, a hand-off, an assignment, opening the next task — none of these is a criterion, and the create/update tools drop them. Every criterion must be checkable from the running product (or the delivered document) alone, without reading the card's history.
- **Criteria are structured, not prose.** Pass `acceptance_criteria: ["Given …, When …, Then …", …]` — each item becomes a checkbox the implementer ticks with `set_criterion_completed` and that QA and you then rule on with `review_criterion`. A criteria block written as markdown text produces a task with an EMPTY checklist, so the task can never be verified complete.
- **Tag repository and project.** Both accept names, so there is no reason to skip them and no reason to ask the stakeholder — look them up. To file a task that already exists, use `update_board_task` with `project`.
- **One role, one deliverable.** Never bundle backend + frontend in one task.
- **Ordering is an argument, not a sentence.** analiz (if needed) → backend → frontend/mobile → qa. Declare each cross-task dependency with `blocked_by` (nobody starts this until those are done) and, where the order also applies to shipping, `deploy_depends_on` (this is not released until those are live). Both point the same way: this task comes after the ones you list. A "Depends on: …" line in the description is worth writing for the human, but it enforces nothing on its own, and a cycle is refused at creation.
- **Ordered tasks still all go on the board.** A blocked task parks itself and is picked up automatically when its blocker lands, so there is no reason to hold work in `backlog` to fake an order.
- **Point at the analysis.** If the work came out of an analiz task, set `derived_from`. The architect normally does this; when you create the task yourself, you do.
- **Architect-created tasks:** most implementation tasks are created by the system-architect after the human approves an analiz — you write tasks directly only for clear, small, no-analiz work.

## Worked Example

```
create_board_task(
  title:       "Add task export endpoint",
  task_type:   "task",
  column:      "backlog",
  assignee:    "backend-developer",
  repository:  "tasktrooper",
  project:     "Task export",
  description: "As a project member, I want to export my project's tasks to CSV, so that I can share them.
                Out of scope: the web download button (separate frontend task). Depends on: none.",
  derived_from: ["A-12"],
  technical_description: "New GET /api/v1/projects/:id/tasks/export in internal/adapter/http/handler_task.go.
                Streams text/csv, reuses repository.Service.ListTasks. No schema change.
                Ref: the implementation plan attached to analiz task A-12.",
  acceptance_criteria: [
    "Given a member of the project, When they GET /api/v1/projects/:id/tasks/export, Then they get 200 + text/csv with header id,title,status,created_at",
    "Given a user without access to the project, When they GET the same URL, Then they get 403 and no data is returned",
    "Given an invalid project id, When they GET the endpoint, Then they get 400"
  ]
)
```

The web download-button task is a SEPARATE frontend task, and its order is declared rather than described:

```
create_board_task(
  title:             "Add export button to the project board",
  assignee:          "frontend-developer",
  repository:        "tasktrooper-web",
  derived_from:      ["A-12"],
  blocked_by:        ["T-1"],   # the endpoint task — nobody starts this until it is done
  deploy_depends_on: ["T-1"],   # and it must not ship before the endpoint is live
  ...
)
```

## Common Mistakes

- "Add export API and button" → two tasks.
- Aspirational AC ("works well").
- No assignee, or a role that can't do the work.
- An "Acceptance Criteria" heading inside `description` → the checklist is empty; pass `acceptance_criteria` instead.
- Technical detail written into both `description` and `technical_description` → keep it in `technical_description` only.
- An order stated only as "Depends on: the backend task" → nothing enforces it; pass `blocked_by` / `deploy_depends_on`.
- A task created out of an analysis with no `derived_from` → the developer has no route to the spec, because the spec is a document on the analiz task and not a file in the repo.

## Red Flags

- AC mentions two layers/repos.
- No error/auth criterion.
- The created task comes back with an empty `acceptance_criteria` array → you wrote them as prose. Fix it with `update_board_task`.
