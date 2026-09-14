---
name: performance-awareness
category: quality
description: Use when working any board task - explains how your performance score moves and how to protect it through the review, QA, and UAT gates
---

# Performance Awareness

## Overview

Every agent carries a performance score (0–100, starts at 100). It is not decoration: it reflects how much rework your output causes downstream. A task that sails through code_review, QA, and UAT protects your score; one that bounces back at any gate costs you. The score rewards getting it right the first time, which is exactly what the skills you already have are for.

**Core principle:** The cheapest revision is the one that never happens. Every gate you pass clean is points kept; every bounce is points lost AND time lost.

## How the score moves

| Event | Change |
|-------|--------|
| Your completed task reaches **done** | +5 |
| Your completed task reaches **released** | +5 |
| Task returned to **need_revision** from code_review, ready_for_qa, or in_qa | −10 per revision |
| **PM UAT** or **Human UAT** fails on your work | −5 per failure |

The asymmetry is deliberate: one revision (−10) wipes out two clean releases (+10). Avoiding a bounce is worth far more than finishing fast.

## The gates your work passes

```
in_progress → code_review → ready_for_qa → in_qa → pm_uat → human_uat → done → released
              (architect)                  (QA)    (PM)     (human)
```

A defect caught at code_review costs −10. The SAME defect that slips to in_qa or UAT still costs you and now also burns QA/PM/human time. Catch your own defects before handoff — that is what the score is measuring.

## To protect your score

1. **Read ALL acceptance criteria before starting.** Most bounces are unmet AC, not bugs.
2. **Self-check each AC** against fresh, executed evidence before moving the task forward (see verify-before-done).
3. **Never guess a product decision** — add_task_comment with numbered questions instead. A wrong guess becomes a UAT failure.
4. **On need_revision, address EVERY numbered point** and add a guard test (see root-cause-debugging). A partial fix bounces again for another −10.
5. **Run the build and affected tests in THIS run** before claiming completion.

## Worked Example

Two developers each ship 5 tasks in a week.

- Dev A rushes: 5 tasks to code_review fast, 3 bounce for unmet AC (−30), fixes them, all 5 eventually released (+25). Net for the week: **−5**, plus the architect reviewed 8 times.
- Dev B self-checks every AC before handoff: 5 tasks, 0 bounces, all released (+25 done +25 released). Net: **+50**, architect reviewed 5 times.

Same 5 tasks shipped. The difference is entirely self-verification before handoff.

## Red Flags

- Moving a task to code_review to "see if it passes review" — review is not your test suite.
- Skipping an AC because "it's probably fine."
- Leaving a revision comment partially addressed.
