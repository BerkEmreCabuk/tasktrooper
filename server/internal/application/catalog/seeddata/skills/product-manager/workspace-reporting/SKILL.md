---
name: workspace-reporting
category: pm
description: Use when reporting board status or progress - aggregate with get_board_summary rather than paging every task, then translate the numbers into an outcome-language update
---

# Workspace Reporting

## Overview

A status report is a translation from board data to stakeholder outcomes. The failure mode is paging through every task (slow, noisy) or dumping raw counts without interpretation.

**Core principle:** Aggregate first with `get_board_summary`, then turn the numbers into an outcome update.

## The Process

1. Call `get_board_summary` — one call returns the total task count and counts grouped by column, type, and priority.
2. Translate into a human update: what's in progress, what's waiting (backlog/todo), what's blocked or in review (need_revision/pm_uat), and the biggest bucket.
3. Only drill into `list_board_tasks` when the stakeholder asks about a specific task or you need titles/assignees.
4. Report in outcome language (see stakeholder-communication).

## Worked Example

`get_board_summary` → {total 24; in_progress 3, code_review 2, need_revision 4, backlog 10, done 5}.

Report: "24 tasks in total. 3 are in active development, 2 are in architecture review. 4 went back for revision — that's where the biggest risk is. 10 are queued and 5 are done." Not: a wall of 24 task titles.

## Common Mistakes

- Paging `list_board_tasks` for a status question `get_board_summary` answers in one call.
- Reporting raw counts with no interpretation or risk callout.
- Listing every task when the stakeholder asked "how are we doing?"

## Red Flags

- A status update with no mention of the blocked/revision bucket.
- Twenty task titles where four sentences would do.
