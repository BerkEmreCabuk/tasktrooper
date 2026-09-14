---
name: project-split-decomposition
category: architecture
description: Use when an approved analiz spans more than one project or repository - split the work per project and open one implementation task per project carrying its own plan slice
---

# Project Split Decomposition

## Overview

Real features cross project boundaries: an API in the backend repo, a screen in the web repo, a screen in the mobile repo. After the human approves the analysis, you split the work along project lines and open **one implementation task per project**, each carrying the slice of the plan that project needs. A developer working the web task should never have to read the backend repo's plan to do their job.

**Core principle:** One project, one task, one self-contained plan slice. The split follows the deployable unit, not the feature.

**REQUIRED SUB-SKILL:** task-decomposition (per-task structure, AC, assignees). This skill decides the BOUNDARIES; task-decomposition fills each task in.

## When to Use

Use after the human approves an analiz (see analiz-human-review-gate) whenever the plan touches more than one project/repository. A single-project change skips straight to task-decomposition.

## How to Split

1. **List the projects the plan touches.** Each repository is a project boundary: backend-api, web, mobile, an automation/test project, etc.
2. **One implementation task per project.** Never bundle two projects into one task — they build, test, and deploy independently.
3. **Within a project, split by layer only if independently deliverable.** A backend task may cover the whole vertical slice for that repo; split further only where a reviewer could accept one part and reject another.
4. **Write each project's plan slice into its task.** Copy the relevant plan sections, file paths, and the Interfaces blocks that task consumes/produces — verbatim, not "see the main plan." The task is self-contained.
5. **Order by cross-project dependency, in the arguments.** The project that produces an interface goes before the project that consumes it. On the consuming task set `blocked_by: ["<producer key>"]` (nobody starts it until the producer is done) and `deploy_depends_on: ["<producer key>"]` (it may not be released until the producer is live). Both are enforced — a prose "depends on the backend task" is not. See task-decomposition, "Ordering Is an Argument, Not a Sentence".
6. **Point every task back at the analysis.** `derived_from: ["A-N"]` on each one. The spec and the plan live as documents on that analiz task, so this is the only route each developer has to them.

## The Cross-Project Contract

The one thing that MUST be identical across the split is the interface between projects — the API contract. Define it once in the producing task and copy the exact request/response shape into the consuming task's plan slice. A field named `taskId` in the backend task but `task_id` in the web task is a guaranteed integration bug.

## Worked Example

Approved analiz: "Users can export a project's tasks to CSV from web and mobile."

Split into three tasks:

| Task | Project | Assignee | Ordering arguments | Scope slice |
|------|---------|----------|-----------|-------------|
| Add task export endpoint (T-1) | backend-api | backend-developer | `derived_from: ["A-12"]` | `GET /api/v1/projects/:id/tasks/export` → CSV; plan slice with handler, service, test |
| Add export button to web board | web | frontend-developer | `derived_from: ["A-12"]`, `blocked_by: ["T-1"]`, `deploy_depends_on: ["T-1"]` | Calls the endpoint, downloads the file; plan slice with component + api client |
| Add export action to mobile board | mobile | mobile-developer | `derived_from: ["A-12"]`, `blocked_by: ["T-1"]`, `deploy_depends_on: ["T-1"]` | Same endpoint, native share sheet; plan slice with screen + api client |

The exact endpoint path and CSV column order are defined in the backend task and copied into both consumer tasks' plan slices. All three go to `todo` together: the two consumers park themselves behind T-1 and are picked up automatically when it lands, and neither can be released ahead of it.

## Common Mistakes

- One task titled "add export to web and mobile" — two projects, must be two tasks.
- A consumer task that says "see the backend plan for the contract" — copy the contract in; the developer only sees their own task.
- Splitting a single-repo change across three tasks that can't be reviewed independently — over-decomposition.
- Mismatched field names between the producing and consuming task's plan slices.

## Red Flags

- A consumer task with no `blocked_by` / `deploy_depends_on` on its producer → the order exists only in your head, and the frontend can start (and ship) before the API it calls.
- A task with no `derived_from` → the developer cannot reach the plan slice you wrote for them.
- A task's plan slice references files in a repository the task doesn't own.
- Two tasks would edit the same repository's same files → they are really one task.
- The interface shape differs between the producer and consumer tasks.
