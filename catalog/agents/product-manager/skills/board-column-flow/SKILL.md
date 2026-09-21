---
name: board-column-flow
category: pm
description: Use when moving tasks or reporting status - the kanban column semantics, which columns are human/architect/QA gates, and where the PM actually acts
---
# Board Column Flow

## Overview

The board has more columns than the PM acts in. Knowing which columns belong to the human, the architect, and QA keeps you from moving a task through a gate that isn't yours.

## The columns

```
backlog → todo → in_progress → analiz_review → code_review
        → ready_for_qa → in_qa → pm_uat → human_uat → done → released

need_revision ← any gate that rejects (code_review, in_qa, pm_uat, human_uat)
              → back to in_progress' owner, then forward again from code_review
```

(`analiz_review` and `code_review` are gates for specific work; a given task only passes through the columns its type needs. `need_revision` is not a stage in the line — it is where every gate sends work back.)

## Who owns each gate

| Column | Owner | PM action? |
|--------|-------|-----------|
| backlog | PM | Yes — create tasks here by default; stakeholder prioritizes |
| todo | assignee agent (auto) | Move approved tasks here to start them |
| in_progress | developer | No |
| **analiz_review** | **human** | **No** — the human approves the architect's plan (→ done) or rejects it (→ need_revision) |
| code_review | system-architect | No — architect advances to ready_for_qa or need_revision |
| ready_for_qa | QA (queue — QA takes it into in_qa) | No |
| in_qa | QA (testing in progress) | No |
| need_revision | developer / architect | Read the reason; clarify AC if the gap is a requirements ambiguity, else leave the owner to fix |
| **pm_uat** | **PM** | **Yes** — verify against AC using QA's evidence (see pm-uat-review) |
| human_uat | stakeholder | No — stakeholder reviews |
| done / released | — | Terminal; released per project convention |

## PM action points (only these)

- **backlog:** create tasks here; the stakeholder reviews/prioritizes before agents pick them up.
- **todo:** move approved tasks here to trigger the assignee.
- **pm_uat:** review against AC. All AC covered by QA evidence → `human_uat` with a "PM UAT passed: [what was verified]" comment. Any gap → `need_revision` with the specific AC gap quoted.

Everything else (analiz_review, code_review, QA columns, human_uat) is another actor's gate — don't move tasks through them.

## Common Mistakes

- Moving an analiz task out of `analiz_review` — that's the human's approval gate.
- Advancing a task in `code_review` — that's the architect's.
- Approving in `pm_uat` by reading code instead of QA's executed evidence.

## Red Flags

- You moved a task through a column not in the PM action-points list.
- A `pm_uat` pass with no QA evidence cited.
