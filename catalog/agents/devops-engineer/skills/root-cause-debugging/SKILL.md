---
name: root-cause-debugging
category: quality
description: Systematic debugging - find the root cause through four phases before attempting any fix
---
# Root-Cause Debugging

## Overview

Random fixes waste time and create new bugs. Quick patches mask underlying issues.

**Core principle:** ALWAYS find the root cause before attempting fixes. Symptom fixes are failure.

## The Iron Law

```
NO FIXES WITHOUT ROOT CAUSE INVESTIGATION FIRST
```

Use this for ANY technical issue: a task returned to need_revision, a failing test, a red pipeline, unexpected behavior. Use it ESPECIALLY under time pressure — systematic is faster than guess-and-check thrashing.

## The Four Phases

Complete each phase before the next.

### Phase 1 — Root cause investigation
1. **Read the feedback completely**: the reviewer/QA/pipeline comment on the task, the full error output, stack traces, exact files/lines/messages. They often contain the exact answer.
2. **Reproduce consistently** in the task workspace: exact steps, does it happen every time? Not reproducible → gather more data, don't guess.
3. **Check recent changes**: git diff, recent commits on the task branch, config changes.
4. **Multi-component systems** (API → service → database): add diagnostic logging at each component boundary — what enters, what exits — run once, and locate WHICH layer breaks before touching anything.
5. **Trace the bad value backward** from where the error appears to where it originates. Fix at the source, not at the symptom.

### Phase 2 — Pattern analysis
- Find working examples of the same pattern in the codebase (codebase_search, grep_code).
- Read the reference implementation completely — don't skim.
- List every difference between working and broken; don't assume "that can't matter".

### Phase 3 — Hypothesis and testing
- Form a single, specific hypothesis: "I think X is the root cause because Y."
- Test it with the SMALLEST possible change. One variable at a time.
- Didn't work? Form a NEW hypothesis. Do NOT stack more fixes on top.

### Phase 4 — Implementation
1. Write a failing test that reproduces the issue (see tdd-workflow).
2. Implement the single fix that addresses the root cause. No bundled refactoring.
3. Verify: the test passes, no other test breaks, the original symptom is gone.
4. Address EVERY point from the revision comment explicitly — partial fixes come straight back.

## The 3-Fix Rule

If 3 fixes have failed, STOP. Each fix revealing a new problem elsewhere means the architecture or approach is wrong, not the code. Question the pattern — add a task comment describing the architectural concern instead of attempting fix #4.

## Red Flags — STOP and return to Phase 1

- "Quick fix for now, investigate later"
- "Just try changing X and see if it works"
- "It's probably X, let me fix that"
- Proposing solutions before tracing data flow
- Multiple changes at once
- "One more fix attempt" after 2+ failures

## Common Rationalizations

| Excuse | Reality |
|--------|---------|
| "Issue is simple, no need for process" | Simple issues have root causes too; the process is fast for them. |
| "Emergency, no time" | Systematic debugging is faster than thrashing. |
| "I see the problem, let me fix it" | Seeing symptoms is not understanding root cause. |
| "Multiple fixes at once saves time" | You can't isolate what worked, and you create new bugs. |
| "I'll write the test after the fix works" | Untested fixes don't stick. Test first proves it. |
