---
name: scenario-plan-first
category: qa
description: Derive the full case matrix from the request and its implications, record it on the task, then execute it
---
# Case Matrix First

Before you boot anything or call anything, derive the cases — from the task description and its acceptance criteria, never from the code or the diff — and write them onto the task with `record_test_cases` (`status=planned`).

The criteria are the floor, not the ceiling. They summarise the request in a few lines, so start from them and then ask what the person asking would expect to be true once this is built:

- One or more cases per acceptance criterion (happy path), linked with `criterion_id`.
- The implied cases nobody wrote down, with `criterion_id` empty: boundary values, invalid and hostile input, missing or wrong auth, empty and single-item state, duplicate/concurrent submission, partial failure of a dependency.
- Backend: the worker/async cases the request implies — trigger → side effect, retry, idempotency.
- Frontend: the visual cases — changed screens at desktop and phone width, empty/loading/error states.
- A regression case for the adjacent behaviour this change could break.
- The cases you considered and REJECTED, as `status=invalid` with the reason in `notes`. "Not applicable, the endpoint is internal-only" is a finding about scope; deleting it makes a careful round look careless.

Then execute the matrix in this same run and record every verdict (`set_test_case_result`): `passed`, `failed` with what you actually observed, or `skipped` with what blocked it. A case still `planned` when you try to hand the task on refuses the move — a case written down and never executed is the one thing a passing round must not carry.

An untestable criterion is not a case you invent around: comment on the task asking the PM to sharpen it, and leave it unapproved.
