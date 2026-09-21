---
name: spec-authoring
category: architecture
description: Write and self-review a design spec before any implementation plan
---
# Spec Authoring

## Overview

Attach the validated design to the analiz task with `add_task_document`, titled `spec: <YYYY-MM-DD> <topic>`. The task document IS the spec — do not write it to a file and do not commit it; you have no file-writing tool, and shelling out to `echo`/heredoc to fake one is how a run gets killed for repeating itself. The spec is the contract the plan and every implementation task will be checked against.

The document must name real files, symbols, and interfaces you found with `codebase_search` / `grep_code` / `get_repo_tree` / `expand_symbol_context`. A run that attaches an analiz document without a single successful exploration call is rejected by the system, not by a reviewer.

## Structure

Scale each section to its complexity — a few sentences if straightforward, up to 200–300 words if nuanced:

1. **Context / Goal** — the problem, who has it, what outcome closes it.
2. **Architecture** — the chosen approach, and in one line why it beat the alternatives.
3. **Components** — each unit with clear boundaries: what it does, its interface (exact names and types), what it depends on.
4. **Data flow** — how a request/value moves through the components, including where validation happens.
5. **Error handling** — what fails, how each failure surfaces, what the user sees.
6. **Testing approach** — what proves each component works: unit, integration, end-to-end.
7. **Out of scope** — explicitly named items deferred or excluded.

## Writing Rules

- State decisions, not options: the exploration happened in technical-analysis-workflow; the spec records what WILL be built.
- Every interface names its exact functions, parameters, and return types — implementers must not have to invent names.
- Copy project-wide constraints (versions, naming/locale rules, layer boundaries) verbatim — they become the plan's Global Constraints block.

## Worked Example (a Components entry done right)

```markdown
### Component: TaskExporter (application service)
- Does: turns a project's tasks into CSV bytes.
- Interface: `Export(ctx, projectID uuid.UUID) ([]byte, error)`
  - errors: domain.ErrProjectNotFound, domain.ErrForbidden
- Depends on: TaskRepository.ListByProject(ctx, projectID) ([]Task, error)
- Data flow: handler validates projectID → TaskExporter.Export → repo.ListByProject
  → encode CSV (header + one row per task, columns: id,title,status,created_at).
- Out of scope: PDF, scheduled export, column selection.
```

An implementer reading only this can build it: the exact signature, the exact dependency it consumes, the column order, and the errors it raises. That is the bar for every Components entry.

## Self-Review (mandatory, before handoff)

Look at the finished spec with fresh eyes and fix issues inline:

1. **Placeholder scan** — no "TBD", "TODO", incomplete sections, or vague requirements.
2. **Internal consistency** — no section contradicts another; the architecture matches the component descriptions.
3. **Scope check** — focused enough for ONE implementation plan? If not, decompose into sub-specs.
4. **Ambiguity check** — could any requirement be read two different ways? Pick one reading and state it explicitly.

A spec that fails any check is not ready — fix it before invoking implementation-plan-authoring.
