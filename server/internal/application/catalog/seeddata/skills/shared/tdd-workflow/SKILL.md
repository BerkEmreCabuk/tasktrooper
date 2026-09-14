---
name: tdd-workflow
category: testing
description: Test-driven development - write the failing test first, watch it fail, write minimal code to pass
---

# Test-Driven Development

## Overview

Write the test first. Watch it fail. Write minimal code to pass.

**Core principle:** If you didn't watch the test fail, you don't know if it tests the right thing.

**Violating the letter of the rules is violating the spirit of the rules.**

## The Iron Law

```
NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST
```

Wrote code before the test? Delete it. Start over. Don't keep it as "reference", don't "adapt" it while writing tests — delete means delete, implement fresh from the test.

## When to Use

Always: new features, bug fixes, refactoring, behavior changes. Thinking "skip TDD just this once"? Stop. That's rationalization.

## Red-Green-Refactor

### RED — write one failing test
- One behavior per test, clear name that describes the behavior.
- Test real code; mocks only when unavoidable (external services).
- Bad: a test whose assertions only exercise the mock, not the code under test.

### Verify RED — watch it fail (MANDATORY, never skip)
- Run the test now (go test ./pkg/... or npm test).
- Confirm it FAILS (not errors) and fails because the feature is missing — not a typo or compile error.
- Test passes immediately? It tests existing behavior — fix the test.
- Test errors? Fix the error and re-run until it fails correctly.

### GREEN — minimal code
- Write the simplest code that makes the test pass. Nothing extra (YAGNI).
- No extra options, no speculative parameters, no "while I'm here" improvements.

### Verify GREEN — watch it pass (MANDATORY)
- Run the test and the surrounding suite. All green, output pristine (no warnings).
- Test fails? Fix the code, not the test. Other tests fail? Fix now, not later.

### REFACTOR — only while green
- Remove duplication, improve names, extract helpers. No new behavior.
- Then write the next failing test.

## Worked Example

Feature: `slugify(title)` lowercases and hyphenates.

```
RED   test: slugify("Hello World") == "hello-world"
      run → FAIL: slugify is not defined            ← watched it fail, right reason
GREEN func slugify(s) { return strings.ReplaceAll(strings.ToLower(s), " ", "-") }
      run → PASS                                     ← watched it pass
RED   test: slugify("A  B") == "a-b" (collapse runs)
      run → FAIL: got "a--b"                         ← new behavior, fails first
GREEN collapse whitespace before replacing
      run → PASS
REFACTOR extract the whitespace regex, suite still green
```

Each behavior earned its own failing test first. The second test caught a real gap the first implementation missed — which is the whole point of writing it before the code.

## Bug Fixes

A bug fix starts with a failing test that reproduces the bug. The test proves the fix and prevents regression. Never fix a bug without a reproducing test.

## Common Rationalizations

| Excuse | Reality |
|--------|---------|
| "Too simple to test" | Simple code breaks too. The test takes 30 seconds. |
| "I'll test after" | Tests written after pass immediately and prove nothing. |
| "Already manually tested" | Ad-hoc is not systematic: no record, cannot re-run. |
| "Deleting X hours is wasteful" | Sunk cost fallacy. Unverified code is technical debt. |
| "Keep it as reference" | You will adapt it — that is testing after. Delete it. |
| "TDD will slow me down" | TDD is faster than debugging in review/QA and revision cycles. |
| "Test is hard to write" | Hard to test = hard to use. Simplify the design. |

## Red Flags — STOP and start over

- Code written before its test
- Test passes on the first run
- You cannot explain why the test failed
- "Tests later", "just this once", "this is different because..."

All of these mean: delete the code, start from the test.

## Checklist before moving the task forward

- [ ] Every new function/behavior has a test
- [ ] Watched each test fail for the expected reason
- [ ] Wrote minimal code to pass
- [ ] Whole suite green, output pristine
- [ ] Edge cases and error paths covered

Can't check every box? You skipped TDD. Start over.
