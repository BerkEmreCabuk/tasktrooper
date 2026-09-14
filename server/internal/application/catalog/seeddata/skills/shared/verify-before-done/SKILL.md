---
name: verify-before-done
category: quality
description: Never claim done or move a task forward without fresh verification evidence from this run
---

# Verification Before Completion

## Overview

Claiming work is complete without verification is dishonesty, not efficiency.

**Core principle:** Evidence before claims, always.

## The Iron Law

```
NO COMPLETION CLAIMS WITHOUT FRESH VERIFICATION EVIDENCE
```

If you haven't run the verification command in THIS run, you cannot claim it passes. A previous run, "should pass", or "looks correct" is not evidence.

## The Gate Function

Before claiming any status or moving any task forward:

1. IDENTIFY — what command proves this claim?
2. RUN — execute the full command, fresh and complete.
3. READ — the full output: exit code, failure count, warnings.
4. VERIFY — does the output actually confirm the claim? If no: state the real status with evidence.
5. ONLY THEN — make the claim, with the evidence.

Skipping any step is lying, not verifying.

## What Each Claim Requires

| Claim | Requires | Not sufficient |
|-------|----------|----------------|
| Tests pass | Test command output: 0 failures | Previous run, "should pass" |
| Build succeeds | Build command: exit 0 | Linter passing, logs look fine |
| Bug fixed | Re-run the original symptom: passes | Code changed, assumed fixed |
| AC met | Line-by-line check of every acceptance criterion | Tests passing |

Tests passing is NOT the same as requirements met — re-read every acceptance criterion and confirm each one is actually satisfied.

## Board Handoff

When the checks pass and every AC is confirmed, your closing action is your run's final MESSAGE: what you changed and how you verified it — the checks you actually ran and what they reported. Keep it short, and keep it out of the comments: a task whose work went through cleanly gets no comment at all, because the diff, the PR, the pipeline result and the ticked criteria already carry it. Comment only when something is wrong or still open.

You do NOT move the task. A run that ends with a green build and a real diff on the branch is moved to **code_review** by the control plane, which also opens the pull request — ready for review, never a draft — and starts the pipeline. Never plan a step for that move.

If you cannot show fresh passing output, the task is not done — say what actually failed instead. And do not re-run a check you already have fresh output for: once a build has passed on the code as it stands, running it again proves nothing and costs the run.

## Red Flags — STOP

- Using "should", "probably", "seems to"
- Expressing satisfaction before verification ("Done!", "Perfect!")
- Finishing a run without having run the checks in it
- Relying on partial verification ("the linter passed")
- Running the same check twice over unchanged code — that is not rigor, it is a loop

## Rationalization Prevention

| Excuse | Reality |
|--------|---------|
| "Should work now" | RUN the verification. |
| "I'm confident" | Confidence is not evidence. |
| "Just this once" | No exceptions. |
| "Partial check is enough" | Partial proves nothing. |
| "I'm tired / it's late in the task" | Exhaustion is not an excuse. |
