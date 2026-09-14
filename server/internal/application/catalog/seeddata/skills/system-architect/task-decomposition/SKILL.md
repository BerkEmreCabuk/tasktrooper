---
name: task-decomposition
category: architecture
description: Split a finished plan into per-repo, per-layer implementation board tasks
---

# Task Decomposition

## Overview

After the plan is written and self-reviewed, turn it into implementation board tasks with create_board_task. Decomposition quality decides whether developers can work in parallel without stepping on each other.

## Slicing Rules

- **One repository + one layer per task** (backend / frontend / mobile). Never bundle layers — a task that says "add the API and the UI" is two tasks.
- Each task maps to one or more plan tasks that form an independently deliverable unit: its tests can pass and its code can be reviewed without waiting for a sibling.
- Order by dependency: backend API before the frontend/mobile that consumes it. Declare it in the ARGUMENTS (see "Ordering is an argument" below), and write the human-readable "Depends on: <task title> — consumes POST /api/v1/..." line in the description as well — but never only the line, because prose enforces nothing.

## Each Task Must Contain

Each of these is a separate `create_board_task` field. Never paste one field's content into another — the board renders them in their own sections, and a duplicated copy goes stale the moment the real field is edited.

- **`title`:** action-object format ("Add task export endpoint", not "Export work").
- **`description`** (product only): user story ("As [persona], I want [capability], so that [outcome]") + context + which plan tasks it covers + the dependency line ("Depends on: …").
- **`technical_description`** (technical only): the titles of the analiz task's spec and plan documents (`spec: …`, `plan: …`), the endpoints/files/schema this slice touches, and the **interfaces** — the exact names/types it consumes from and produces for its neighbors, copied from the plan's Interfaces blocks.
- **`acceptance_criteria`:** an array of strings, one observable Given/When/Then per item including error cases — copied or derived from the plan, never aspirational wording. Passing them as an array is what gives the task a real checklist; writing them as prose in `description` leaves it empty and the task can never be verified complete. **Product only:** a criterion is checked against the running system, never against the board — "moved to code_review", "PR opened", "QA notified", "the follow-up task is created" are workflow, and `create_board_task` drops them with the reason in its result.
- **`assignee`:** the matching developer role — backend-developer / frontend-developer / mobile-developer.
- **`derived_from`:** `["A-N"]` — the analiz task this slice came out of. REQUIRED on every task you create from an approved analysis. Your spec and plan are documents on that task and nowhere else; this reference is what feeds them into the developer's run and what makes `list_task_documents A-N` the answer when they need to re-read the plan. Naming the document titles in `technical_description` is not a substitute — a title is not a route.

## Ordering Is an Argument, Not a Sentence

Three orderings, three arguments, all pointing the same way — **this task comes after the ones you list**:

| Argument | Means | What enforces it |
|---|---|---|
| `blocked_by: ["T-1"]` | nobody starts this task until T-1 is done or released | the dispatcher parks the card in `blocked` with the reason on it, and picks it up automatically the moment T-1 lands |
| `deploy_depends_on: ["T-1"]` | this task may not be RELEASED until T-1 is live in production | the release gate refuses the deploy and comments why; the ordering is also written into this task's `before_deploy` runbook for you |
| a "Depends on: …" line in `description` | a human reading the card understands the shape | nothing |

- The backend task a frontend consumes is usually **both**: the frontend cannot be written before the contract exists, and it must not ship before the API does. Set both arguments.
- Two tasks that were developed in parallel and never blocked each other can still have a hard shipping order — that is `deploy_depends_on` alone.
- A cycle is refused at creation with the chain that causes it. If you hit one, the split is wrong: two tasks that each have to go first are one task.
- Anything that has to happen around the deploy — a migration to run first, a flag to flip, a cache to warm, how to undo it — goes in `before_deploy` / `after_deploy` / `rollback_plan` on the task that owns it. Those fields are posted on the card automatically when the release is dispatched and when it lands. A pre-deploy step written as a comment is one nobody sees at deploy time.

## Board Mechanics

Task creation happens ONLY after the human approves the analysis (see analiz-human-review-gate). The analiz task is in the `done` column when you run this — that column IS the approval signal.

1. create_board_task per slice, column=todo, with `assignee` set to the matching developer role (REQUIRED — an unassigned task is never dispatched and sits idle) and `derived_from` set to this analiz task. Call `list_team` to confirm the valid role names. Developers pick up their assigned tasks autonomously — no further human gate on implementation tasks.
2. Create them in dependency order — the producer first — so each dependent can name the task it waits for by key in `blocked_by` / `deploy_depends_on`. If you only realise an order after the fact, `update_board_task` with `blocked_by` adds it.
3. Putting every slice in `todo` at once is correct even when they are ordered: a task whose blocker is open is parked automatically and released the moment the blocker lands. Holding tasks back in `backlog` to fake an order is what the arguments replace.
4. add_task_comment on the analiz task listing every created task: title, assignee, project, and its order (what it waits for and what ships after it).
5. Move the analiz task to **released** — you are finished with it. (You never move it to `done`; the human does that as their approval action.)

## Red Flags

- Creating tasks while the analiz task is still in `analiz_review` → you skipped the human gate.
- A created task that comes back with an empty `acceptance_criteria` array → you wrote the criteria as prose; fix it with `update_board_task`.
- A created task that comes back with `dropped_criteria` → you wrote board steps as criteria; replace them with statements about the product, not with the same sentence reworded.
- A task whose AC mention two layers or two repositories.
- A task the assignee cannot start because an interface it consumes is defined nowhere.
- A created task with no `derived_from` → its developer has no route to your spec and plan. Fix it with `update_board_task` before you release the analiz task.
- An order that exists only as "Depends on: …" prose → nothing enforces it; add `blocked_by` / `deploy_depends_on`.
- A cycle refused at creation → your split is wrong, not the board. Merge the two tasks or move the shared piece into its own task that goes first.
- Moving the analiz task to `done` yourself → `done` is the human's approval move; you move it to `analiz_review` then `released`.
