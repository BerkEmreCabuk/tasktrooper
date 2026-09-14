---
name: incremental-commits
category: workflow
description: Break work into bite-sized steps with their own test cycle and commit each one
---

# Incremental Commits

## Overview

Work in bite-sized steps, not one large change. Small commits keep the diff reviewable at code_review and make revisions cheap.

## The Process

1. Before coding, turn the task into a short ordered list of steps. Each step is ONE action worth 2–5 minutes:
   - "Write the failing test" — a step
   - "Run it and watch it fail" — a step
   - "Write minimal code to pass" — a step
   - "Run the tests, all green" — a step
   - "Commit" — a step
2. Commit after each green step on your task branch.
3. Each commit is one logical change: the test and the code that satisfies it belong together; unrelated changes never share a commit.
4. Commit message: imperative summary line; explain WHY in the body when it is not obvious from the diff.

## Rules of Thumb

- Never accumulate a large uncommitted change — if the diff is hard to describe in one sentence, you missed a commit point.
- A step should end with an independently verifiable state (test green, build passing).
- Setup/scaffolding folds into the step whose deliverable needs it; don't commit empty skeletons.
- If a step balloons mid-way, stop, commit what is green, and re-slice the rest.

## Quick Reference

| Situation | Commit boundary |
|-----------|-----------------|
| Test + code that satisfies it | One commit together |
| Two unrelated behaviors | Two commits |
| Refactor + behavior change | Separate commits (refactor first, while green) |
| Setup/scaffolding | Folded into the step that needs it — no empty-skeleton commits |
| Formatting-only change | Its own commit, never mixed with logic |

## Worked Example

Task: add a `priority` field to task creation. A good branch history:

```
feat: add priority column migration
test: expect create to persist priority
feat: persist priority in create handler
test: default priority to "medium" when omitted
feat: default omitted priority to medium
```

Five focused commits. When QA later reports "omitted priority crashes," the fix and the guard test land on top as commit six — nobody re-reads the migration. Compare to one `feat: add priority` blob, where the same fix forces a reviewer back through the entire change.

## Why

- The architect reviews your branch diff at code_review: a series of focused commits is reviewable; a 1000-line blob is not.
- When a revision comes back, small commits localize what to change.
- If an approach fails, you lose one step, not the whole task.

## Red Flags

- The diff is hard to describe in one sentence → you missed a commit point.
- `git status` shows changes across five unrelated areas → you batched.
- You are about to commit with message "wip" or "fixes" → the step wasn't a clean logical unit.
