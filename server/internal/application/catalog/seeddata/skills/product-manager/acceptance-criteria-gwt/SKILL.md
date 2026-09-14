---
name: acceptance-criteria-gwt
category: pm
description: Use when writing acceptance criteria for a task - express each as an observable Given/When/Then that QA can execute, including negative cases
---

# Acceptance Criteria (Given/When/Then)

## Overview

Acceptance criteria are the contract QA tests the product against and the PM verifies at UAT. If a criterion isn't executable, it can't be verified — and unverifiable criteria are where defects hide.

**Core principle:** Every criterion is an observable behavior QA can execute with a command or a click path. No aspirational adjectives.

## The shape

`Given [precondition], When [action], Then [measurable result].`

## Where they go

Criteria live in the board task's own `acceptance_criteria` field — an array of strings, one criterion per item, passed to `create_board_task` (or `update_board_task` to replace the whole list). Each item becomes a checkbox the implementer ticks with `set_criterion_completed`, and that QA (in ready_for_qa/in_qa) and you (in pm_uat) then rule on independently with `review_criterion`. The tick is the implementer's claim; the verdict is the proof, and only reviewers can record one — QA and PM do not hold `set_criterion_completed`.

Never write them into `description`. A criteria block pasted as markdown leaves the task's checklist empty: nothing to tick, nothing the move guard can check, and the prose copy goes stale the moment the real criteria are edited.

## Never a criterion: board actions

A criterion states what the finished WORK is, never what the board does around
it. These are not criteria and the tools drop them:

| Not a criterion | Why | Write instead |
|---|---|---|
| "Task moved to ready_for_qa / analiz_review / done" | a column move is workflow; the agent's role already tells it where the card goes | the behaviour that makes the task ready |
| "Implementation tasks are created" | they are opened AFTER this task is approved — a criterion nobody can tick before the hand-off keeps the card out of done forever | what the plan those tasks come from must contain |
| "Spec attached with add_task_document" / "questions asked with add_task_comment" | naming the tool describes the mechanics, and an attached document is not a good one | what the document must say |
| "Presented for human approval" / "assigned to the developer" | hand-offs are the flow's job | nothing — the flow does it |

The test: could a reviewer check it by looking only at the product and the
documents, without opening the board's history? If not, it is not a criterion.

## Rules

- **Observable + measurable.** "Then a 422 with message 'title too long'" — not "Then it handles it gracefully."
- **At least one negative criterion per feature:** invalid input, unauthorized access, empty state.
- **3–7 criteria per task.** More means the task should be split.
- **No 'fast'/'user-friendly'/'robust'** — replace with a number or a concrete behavior.

## Worked Example

Feature: reject task titles over 200 chars.

```
AC1 (happy)   Given a create-task form,
              When I submit a 200-char title,
              Then the task is created (201).

AC2 (boundary/negative)
              Given a create-task form,
              When I submit a 201-char title,
              Then I get 422 with message "title must be at most 200 characters"
              and no task is created.

AC3 (auth/negative)
              Given a user without write access to the project,
              When they POST a task,
              Then they get 403 and no task is created.
```

Each is a command QA runs (`curl` with a 200/201/short title) with an exact expected status and message — nothing to interpret at UAT.

## Common Mistakes

- A board action as a criterion (see above) — the most common one is "the task is moved to X".
- Aspirational wording ("intuitive", "fast").
- Only happy-path criteria, no negative.
- 12 criteria on one task → split it.
- A "Then" that isn't observable from outside the code.
- Criteria pasted into `description` as a markdown list instead of passed as `acceptance_criteria`.

## Red Flags

- QA asks "how do I test this?" → the AC isn't executable.
- No negative/auth criterion.
- An adjective in a "Then".
- A criterion that names a board tool or a column the card must reach.
