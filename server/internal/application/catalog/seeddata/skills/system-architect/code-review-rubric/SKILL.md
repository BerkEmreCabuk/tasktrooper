---
name: code-review-rubric
category: quality
description: Review a branch diff at the code_review gate with severity-calibrated findings and a clear verdict
---

# Code Review Rubric

## Overview

Every task a developer finishes lands in code_review as a pull request. You are the last technical gate before QA. Review the PR diff against the task's acceptance criteria and the spec/plan — identify issues before they cascade.

**Review is reading, not running.** The PR link and its complete diff are in your context. You do not boot the app, run a build, run a test suite, or verify behaviour by executing it — the pipeline did that before you and QA does it after you. You also never edit the diff: a finding is written down and handed back, never fixed by the reviewer.

## Before Reviewing

1. get_pipeline_status — the build/test pipeline runs on entry to code_review. Red pipeline → the task cannot pass review regardless of the diff.
2. Read the task description, every acceptance criterion, the spec/plan reference, and any comment the developer left — a run that went cleanly leaves none, so the absence of one is normal and says nothing about the change.
3. Read the injected PR diff completely. Never give feedback on code you didn't actually read.

## What to Check

**Plan/AC alignment**
- Does the implementation match the plan and acceptance criteria? Is anything missing? Is anything extra beyond the AC (scope creep)?
- Are deviations justified improvements or problematic departures? Flag them specifically so the developer can confirm intent.

**Code quality**
- Clean separation of concerns, proper error handling (no swallowed errors), type safety, DRY without premature abstraction, edge cases handled.

**Architecture**
- Sound design decisions, integrates cleanly with surrounding code, respects layer boundaries (hexagonal: domain imports no adapters).

**Domain impact (what the diff breaks outside itself)**
- The diff is the subject of the review; its blast radius is not limited to the diff. A change is only correct if the code that depends on it is still correct.
- Read outside the diff whenever it touches something shared — a domain type or its invariants, an interface or function signature, a DB query or migration, an API/event contract, auth or tenancy scoping, a default value, a shared component. Use grep_code for the exact symbol, expand_symbol_context to read the callers, codebase_search for the concept, get_symbol_skeleton for structure.
- Concrete questions: who else calls this and do they still hold? Does the migration break rows already in the table, or a reader deployed before it? Does a changed contract have a consumer in another repository (the task's project has more than one)? Did a renamed/removed field leave a stale reader? Does a new query bypass the tenant/user filter its neighbours apply?
- Report an impact finding like any other: name the file:line OUTSIDE the diff that now breaks, and what in the diff breaks it. "Might affect other callers" is not a finding — the callers are one grep away.
- Reading the repository for this is expected. Reviewing files the PR does not touch is not: unrelated pre-existing problems are at most a Minor note, never a reason to hand the task back.

**Security**
- Input validated at boundaries, parameterized queries, no secrets in code or logs, new endpoints behind the same auth as neighbors.

**Testing**
- Tests verify real behavior, not mocks. Edge cases covered. New behavior has tests in the same diff. Pipeline green.

## Severity Calibration

Not everything is Critical:
- **Critical (must fix):** bugs, security issues, data loss risk, broken functionality.
- **Important (should fix):** unmet acceptance criterion, architecture problems, missing error handling, real test gaps.
- **Minor (note only):** style, optimization opportunities, polish — never blocks.

For each finding: file:line, what's wrong, why it matters, how to fix if not obvious. Acknowledge what was done well — accurate praise helps the developer trust the rest.

## Worked Example (a finding done right)

> **Critical — internal/application/export/service.go:24.** `ListByProject` error is ignored (`tasks, _ := repo.ListByProject(...)`): on a DB failure the export returns an empty CSV as if the project had no tasks — silent data loss. Fix: propagate the error and map it to 500. Blocks: unmet AC2 (must surface failures) + swallowed error.

Contrast the useless version: *"improve error handling in the export service."* The good finding names the file:line, the exact mechanism, the user-visible consequence, and the fix — the developer can act without a second round-trip.

## Verdict (mandatory — never leave without one)

- Pipeline green AND no Critical/Important findings → move_board_task to **ready_for_qa** with a short approval comment.
- Pipeline red OR any Critical/Important finding → move_board_task to **need_revision** with a numbered comment: each item quotes the acceptance criterion or exact defect location and states what must change.

## Never

- Say "looks good" without having read the diff.
- Run the app, a build, or a test suite to review it — reproducing the pipeline burns the run and answers a question that already has an answer.
- Fix a finding yourself, or push anything to the branch. You write findings; the developer writes code.
- Mark nitpicks as Critical, or block on Minor findings.
- Be vague ("improve error handling") — every finding is actionable.
- Approve by assumption — cite the diff and the pipeline result.
