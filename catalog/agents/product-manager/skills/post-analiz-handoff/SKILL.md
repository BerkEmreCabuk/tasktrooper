---
name: post-analiz-handoff
category: pm
description: Use after the system-architect finishes an analiz - understand the human approval gate and review the implementation tasks the architect creates once the human approves
---
# Post-Analiz Handoff

## Overview

The analiz has a human approval gate. The system-architect does NOT create implementation tasks when it finishes analysis — it presents the plan in the `analiz_review` column, the human approves it, and only then does the architect create the tasks. Your job is to understand this sequence and review the tasks after they appear, not to create them.

## The sequence (who does what)

1. **Architect** writes the spec/plan and moves the analiz task to `analiz_review`, then stops.
2. **Human** reviews the plan in `analiz_review` and moves it to `done` (approve) or `need_revision` (reject). This is the human's decision, not yours — do not move an analiz task out of `analiz_review`.
3. **Architect** (on approval) creates one implementation task per project, lists them in a comment, and moves the analiz task to `released`.

## Your steps (after the architect releases the analiz)

Trigger: the analiz task reaches `released` and its comment lists the created implementation tasks.

1. `list_board_tasks` to find the implementation tasks the architect created.
2. Read the architect's spec/plan with `list_task_documents` on the ANALIZ task — they are documents attached to it, never committed to the repository — and read the task comments.
3. Check each created task carries `derived_from` pointing at the analiz task. Without it the developer working that task is never given the spec, and the plan the architect wrote is unreachable from the work it describes. Fix a missing one with `update_board_task`.
4. Check the order is declared, not just described. A frontend task that consumes a new endpoint should carry `blocked_by` and `deploy_depends_on` naming the backend task; a "Depends on: …" sentence in the description enforces nothing. Add the missing ones with `update_board_task` — `blocked_by` adds to whatever is already there.
5. If stakeholder decisions were logged as questions during analysis, confirm answers exist.
6. Review scope and priority; add missing acceptance detail via `update_board_task` if needed — never duplicate or re-create the architect's tasks.
7. Tell the stakeholder which implementation tasks are queued, in what order, and what happens next. A task sitting in `blocked` because its blocker is still open is on schedule, not stuck — it is picked up automatically.

## Common Mistakes

- Creating implementation tasks yourself — the architect owns decomposition, gated on human approval.
- Moving an analiz task from `analiz_review` to `done` — that is the human's approval action.
- Expecting the tasks to exist as soon as the analiz hits `done` — they appear after the architect releases the analiz.

## Red Flags

- You are about to `create_board_task` for work the architect analyzed → stop; the architect creates it after approval.
- You moved an analiz task out of `analiz_review` → that is the human's decision.
