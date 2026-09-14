---
name: implementation-plan-authoring
category: architecture
description: Write a bite-sized, testable implementation plan that assumes the implementer has zero context
---

# Implementation Plan Authoring

## Overview

From the approved spec, attach the plan to the analiz task with `add_task_document`, titled `plan: <YYYY-MM-DD> <topic>` — a task document, not a file in the repo (you do not write or commit files during analysis). Write for a skilled developer who knows NOTHING about this codebase or problem domain and has questionable taste: document every file to touch, every interface, every command. DRY. YAGNI. TDD. Frequent commits — those commits are the implementer's, made later against the plan; the plan itself is never committed.

## File Structure First

Before defining tasks, map every file the plan creates or modifies and what each is responsible for — this locks in the decomposition:

- One clear responsibility per file; prefer smaller focused files over catch-alls.
- Files that change together live together; split by responsibility, not technical layer.
- In existing code follow established patterns — don't unilaterally restructure.

## Task Right-Sizing

A task is the smallest unit that carries its own test cycle and an independently testable deliverable. Fold setup/scaffolding into the task whose deliverable needs it. Split only where a reviewer could reject one task while approving its neighbor.

## Plan Header (mandatory)

```markdown
# <Feature> Implementation Plan

**Goal:** one sentence.
**Architecture:** 2–3 sentences.
**Tech stack:** key technologies.

## Global Constraints
Project-wide rules copied VERBATIM from the spec — version floors, locale/copy
rules, layer boundaries — one line each. Every task implicitly includes this section.
```

## Task Structure

Each task lists:
- **Files:** exact paths — Create / Modify (with line ranges when known) / Test.
- **Interfaces:** Consumes (what it uses from earlier tasks — exact signatures) and Produces (what later tasks rely on — exact names, parameter and return types). An implementer sees only their own task; this block is how they learn neighboring names.
- **Steps** as checkboxes, one action each (2–5 min): write the failing test (show the test code) → run it, expect FAIL with the expected message → write minimal implementation (show the code) → run it, expect PASS → commit (show the command).

## No Placeholders

These are plan failures — never write them:
- "TBD", "TODO", "implement later", "fill in details"
- "Add appropriate error handling" / "handle edge cases"
- "Write tests for the above" without the actual test code
- "Similar to Task N" — repeat the code; tasks may be read out of order
- References to types or functions not defined in any task

If a step changes code, the step shows the code.

## Worked Example (one task, bite-sized)

````markdown
### Task 2: TaskExporter service

**Files:** Create `internal/application/export/service.go`; Test `.../service_test.go`
**Interfaces:**
- Consumes: `TaskRepository.ListByProject(ctx, id) ([]Task, error)` (Task 1)
- Produces: `Export(ctx, projectID uuid.UUID) ([]byte, error)`

- [ ] Step 1 — failing test
```go
func (s *ExportSuite) TestExport_encodesHeaderAndRows() {
    s.repo.EXPECT().ListByProject(mock.Anything, s.pid).Return([]Task{{Title:"A"}}, nil)
    csv, err := s.svc.Export(s.ctx, s.pid)
    s.NoError(err); s.Contains(string(csv), "id,title,status,created_at"); s.Contains(string(csv), "A")
}
```
- [ ] Step 2 — run: `go test ./internal/application/export/... -run TestExport` → FAIL (Export undefined)
- [ ] Step 3 — minimal impl (constructor + Export encoding header+rows)
- [ ] Step 4 — run → PASS
- [ ] Step 5 — commit: `feat: add TaskExporter service`
````

Every step shows the actual code/command — no "add error handling", no "similar to Task 1".

## Self-Review (mandatory)

1. **Spec coverage** — for each spec requirement, point to the task that implements it; add tasks for gaps.
2. **Placeholder scan** — search the plan for the patterns above; fix them.
3. **Type consistency** — names/signatures used in later tasks match their definitions in earlier tasks (`clearLayers()` in Task 3 but `clearFullLayers()` in Task 7 is a bug).

Fix issues inline, then hand off to task-decomposition.
