---
name: release-notes-writing
category: pm
description: Use when a batch of work reaches done/released - write user-facing release notes grouped into New, Fixed, and Changed, in outcome language with task references
---
# Release Notes Writing

## Overview

Release notes tell users what changed for THEM, not what the team did. The failure mode is internal jargon and a flat list of task titles that mean nothing to a user.

**Core principle:** User-visible outcomes, grouped, in plain language.

## Structure

- **New** — user-visible features now available.
- **Fixed** — defects resolved, described by the symptom the user saw.
- **Changed** — behavior changes users must know about (a moved button, a changed default).

Write user-facing language, no internal jargon. Put task keys in parentheses for traceability. Publish via `add_task_document` on the release epic or team summary.

## Worked Example

```markdown
## Release 2026-07-16

### New
- Export a project's tasks to CSV from the board (LLM-142).

### Fixed
- Deleting a task with subtasks no longer shows an error (LLM-150).

### Changed
- Task titles are now capped at 200 characters (LLM-138).
```

Contrast the bad version: *"LLM-142 TaskExporter service; LLM-150 fix nil deref in cascade."* — accurate to the team, meaningless to a user.

## Common Mistakes

- Internal jargon ("nil deref", "N+1").
- Task titles pasted verbatim instead of user outcomes.
- Omitting "Changed" → users surprised by a moved feature.

## Red Flags

- A note a user couldn't understand.
- No task key for traceability.
