---
name: technical-analysis-workflow
category: architecture
description: Turn an analiz request into a grounded technical understanding before any spec or plan
---
# Technical Analysis Workflow

## Overview

Turn an analiz task into a fully-formed technical understanding through investigation, not guessing. The spec and plan you write later are only as good as this grounding.

**Hard gate:** Do NOT write the spec, the plan, or any implementation task until you have explored the actual repository and resolved the ambiguities below. This applies to EVERY analiz task regardless of perceived simplicity — "simple" requests are where unexamined assumptions cause the most wasted developer work.

## The Process

### 1. Clone and explore EVERY relevant repository first
- The analiz task's description lists the projects/repositories the PM believes are involved. Clone/pull ALL of them into your task workspace and review each — a cross-project feature is only understood when every affected repo is read.
- **Do not trust the PM's list as complete.** The PM may miss a repo that also needs to change. Cross-check with the codebase indexes: codebase_search across each repo for the concepts involved, grep_code for the exact symbols/contracts, and follow API contracts to their consumers. If you find an affected repo the PM did not name, pull it and include it in the analysis (and note it in your review summary).
- Explore before proposing anything: codebase_search for concepts, grep_code for exact symbols, get_repo_tree for structure, expand_symbol_context for focused reads.
- Read existing docs and recent commits. Follow existing patterns — never invent a parallel convention.
- **No commits.** You are analysing, not implementing — never commit to any repo. Your entire output (spec + plan) is attached to the analiz task via add_task_document and add_task_comment.

### 2. Understand the intent
- Restate the request in your own words: what outcome is wanted, for whom, and why.
- Assess scope early: if the request describes multiple independent subsystems, decompose it into sub-analyses first — what are the independent pieces, how do they relate, in what order should they be built? Don't refine details of a project that needs splitting.
- Focus questions on: purpose, constraints, success criteria.

### 3. Identify WHAT and WHERE
- Name the units of work, their interfaces, and the exact files/areas each change touches.
- Design for isolation: each unit has one clear purpose, communicates through well-defined interfaces, and can be understood and tested independently. For each unit answer: what does it do, how is it used, what does it depend on?
- If you can't change a unit's internals without breaking its consumers, the boundary is wrong — fix the boundary in the design.

### 4. Propose approaches
- Propose 2–3 approaches with trade-offs; lead with your recommendation and the reasoning.
- YAGNI ruthlessly: strip anything the acceptance criteria don't require.
- Pick one approach and state every ambiguous point explicitly — a requirement readable two ways becomes a defect.

### 5. Resolve unknowns
- Resolve technical unknowns from the code, never by assuming.
- Only genuine PRODUCT decisions escalate: add_task_comment with numbered questions for the PM/stakeholder. Never block on a question the codebase can answer.

## Output

This grounding feeds directly into spec-authoring and implementation-plan-authoring. If you cannot yet name the files to touch and the interfaces between units, the analysis is not done.

## Worked Example

Analiz: "Users can export a project's tasks to CSV." PM named the `backend-api` repo.

1. Clone `backend-api`. `codebase_search "task list endpoint"` → find `TaskHandler` + `TaskRepository.ListByProject` already exist → the export reuses them.
2. Cross-check: `grep_code "api/v1/projects"` across the `web` repo (NOT named by the PM) → the board page will need a download button. Pull `web` too; add it to the split. This is the PM-missed repo the indexes surface.
3. WHAT/WHERE: new `TaskExporter` service (backend) consuming `ListByProject`; new `GET /projects/:id/tasks/export`; a web button calling it.
4. Approaches: (a) stream CSV from the handler, (b) build in a service and return bytes. Pick (b) — testable without HTTP. State it.
5. Unknowns resolved from code (column order = the DTO fields). No stakeholder question needed.

Now the files and interfaces are named → analysis is done, spec/plan can be written.

## Red Flags

- "This is too simple to need analysis" — the design can be short, but it must exist.
- Proposing an approach before reading the relevant code.
- A spec section that says "TBD" or could be read two ways.
- Escalating a question you could answer with grep.
