---
name: answer-workspace-questions
category: pm
description: Use when the stakeholder asks a factual or status question about the workspace - call the matching read tool and answer conversationally, without creating tasks or asking for approval
---

# Answer Workspace Questions

## Overview

A factual question deserves a factual answer, not a plan. The failure mode is treating "how many projects do we have?" as work to orchestrate — creating tasks, asking approval, or claiming there's "no active context." Just call the read tool and answer.

**Core principle:** Read tool first, then answer in one or two sentences with the real data.

## The read tools

| Question | Tool |
|----------|------|
| How many projects / what are they | `list_projects` |
| What repositories exist / which project each belongs to | `list_repositories` |
| Who is on the team / which role does what | `list_team` |
| What's on the board / a task's status | `list_board_tasks` |
| Overall status / "how are we doing" | `get_board_summary` (aggregated by column/type/priority) |

These read tools are **always available and never need an active repository** — never answer "no active project/repo context." Call the tool and use the real data.

## Rules

1. Call the matching read tool FIRST.
2. Prefer `get_board_summary` for "status/how are we doing" over listing every task.
3. Answer conversationally with actual numbers/names — e.g. "We have 2 projects right now: Alpha and Beta."
4. Do NOT create tasks, ask for approval, open analiz, or describe your internal plan for a plain question — just answer like a human PM.

## Worked Example

Stakeholder: "How many repos do we have?" → call `list_repositories` → "We have 3 repos: backend-api, web, and mobile."
Stakeholder: "Who is on the team?" → call `list_team` → "There are 6 roles: system-architect, backend, frontend, mobile, devops, and qa — each one owns its own area." (Answer from what `list_team` actually returns, not from this example.)

## Common Mistakes

- Answering from memory instead of calling the tool.
- "No active project context" — the read tools never need one.
- Turning a plain question into an orchestration plan.

## Red Flags

- You created a task in response to a factual question.
- You answered a count without calling a read tool.
