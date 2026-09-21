---
name: requirements-document
category: pm
description: Use when a product decision or feature needs a written record - what to capture in a task document (PRD, decision record, epic breakdown) and where to attach it
---
# Requirements Documents

## Overview

Some decisions and specs are too big for a task description and need a durable document. The failure mode is either not writing one (decisions get lost) or attaching PRDs to every subtask (noise).

**Core principle:** One document on the parent/epic; task descriptions carry the per-task detail.

## Use add_task_document for

- **PRDs:** context, goals, user stories, AC, out of scope, open questions.
- **Wireframe notes / API contract drafts** before development.
- **Decision records** for significant product choices (what, why, alternatives rejected).
- **Epic-level breakdowns** linking to child tasks.

Attach to the parent/epic task — not every subtask.

## Structure

```
## Context        — the problem, who has it, why now
## Goals          — measurable outcomes
## User Stories   — As [persona], I want [capability], so that [outcome]
## Acceptance Criteria — observable, per acceptance-criteria-gwt
## Out of Scope   — explicitly excluded
## Open Questions — unresolved product decisions
```

## Worked Example

Epic "Reporting v1" gets one PRD via `add_task_document` on the epic task: context (boards are opaque past 200 tasks), goals (3 reports), user stories, AC per report, out of scope (exports, scheduling), open questions (retention window). The child tasks reference it; they don't each re-attach it.

## Common Mistakes

- No document for a significant decision → it's lost in comments.
- A PRD stapled to every subtask → noise.
- Missing "Out of Scope" / "Open Questions" → scope creep and hidden unknowns.

## Red Flags

- A big product choice with no decision record.
- The same document attached to five tasks.
