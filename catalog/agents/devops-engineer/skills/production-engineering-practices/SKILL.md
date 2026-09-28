---
name: production-engineering-practices
category: quality
description: Use when writing or changing production code on any task - the error-handling, security, logging, and performance bar every change must clear before code_review
---
# Production Engineering Practices

## Overview

Acceptance criteria describe the happy path. Production is the unhappy paths: bad input, failed dependencies, concurrent access, hostile users. This skill is the non-negotiable bar every change clears in addition to its AC — the things a reviewer at code_review will bounce you for even when the feature "works."

**Core principle:** Code isn't done when it works; it's done when it fails safely, leaves a trace, and can't be abused.

## The bar

### Error handling
- **No swallowed errors.** Every error is either handled explicitly or propagated with context. An empty catch/`if err != nil { }` that continues is a review-blocking defect.
- Wrap with context at each layer so the log names where it broke: `fmt.Errorf("create task: %w", err)`.
- User-facing errors are actionable and safe; internal detail (stack, ids) goes to logs, never to the client.

### Logging & observability
- Log at the failure site with structured fields (entity id, operation, not free-text).
- **Never log secrets, tokens, passwords, or full request bodies** that may contain them.
- Log the decision, not the novel — one structured line beats ten prose lines.

### Security
- Validate and bound every external input at the boundary: length, type, range, allowed set. Reject early.
- **Parameterized queries only.** No string-concatenated SQL, ever.
- Secrets come from config/env — never hardcoded, never committed, never logged.
- A new endpoint gets the SAME auth/authorization middleware as its neighbors. Copy the guard, don't omit it.

### Performance
- No N+1 queries — batch, join, or preload. A loop issuing one query per row is a defect.
- Bound every result set: pagination or explicit limits on list endpoints.
- Don't load unbounded data into memory; stream or page.

## Quick self-review before code_review

| Check | Pass condition |
|-------|----------------|
| Errors | None swallowed; all wrapped with context |
| Input | Every external field validated and bounded |
| SQL | Parameterized only |
| Secrets | None in code, logs, or commits |
| Auth | New endpoints guarded like neighbors |
| Queries | No N+1; result sets bounded |
| Tests | Behavior change ships with a test in this task |

## Worked Example

Adding `GET /projects/:id/tasks`. The AC just says "return the project's tasks." The production bar adds:
- Validate `:id` is a UUID → 400 on garbage, before any DB call.
- The query filters by `project_id` with a bound parameter and a `LIMIT`/offset — not `SELECT * FROM tasks`.
- The handler reuses the project's auth middleware so a user can't read another tenant's tasks.
- A structured log line on the DB error path with `project_id`.
- Tests: happy path, invalid id → 400, and the pagination bound.

The feature "worked" after the first bullet; it was *done* after all five.

## Handoff

Work on your task branch in the task workspace. When AC is met and the bar above is clear, close the RUN with a final message: what you changed and how you verified it. Do not put it on the card — the architect reviews the diff and QA tests black-box later, and neither needs a summary of a run that went fine. A card comment is for what went wrong or what is still open.

## Red Flags

- "I'll add validation/error handling later" — later is the review bounce.
- A `catch`/`if err != nil` block that does nothing.
- A list endpoint with no limit.
- Copying an endpoint but dropping its auth guard.
