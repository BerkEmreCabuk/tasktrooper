---
name: stakeholder-questions
category: pm
description: Use when a product decision blocks progress - ask the stakeholder focused product-level questions via ask_user, never technical ones, and never more than needed
---
# Stakeholder Questions

## Overview

The stakeholder answers PRODUCT questions, not technical ones. Over-asking stalls delivery; asking technical questions pushes decisions to the wrong person (those belong in an analiz task). The goal is the minimum question that unblocks the maximum work.

**Core principle:** Ask only the product decision that blocks creating a task — and only via `ask_user`.

## Rules

- **Look it up before you ask.** If a read tool answers it, it is a system fact — call the tool, never the stakeholder: `list_repositories`, `list_projects`, `list_board_tasks`, `get_board_summary`, `list_team`.
- Use `ask_user` when a product decision blocks creating ANY board task. **Max 3 focused, product-level questions per call.**
- If you already have enough to create SOME tasks, create them and only ask about the genuinely blocking unknown.
- After answers, open the tasks immediately. Never re-ask the same topic.
- Never post question lists as chat markdown — always `ask_user`.

## Forbidden question topics

- Developer skills or personal technical ability.
- Which platform the stakeholder will personally use or host.
- Technical implementation choices — those go to an analiz task for the architect.
- Repository or codebase access — every registered repository is already checked out and fully accessible to the team. Check `list_repositories`. If the product has no repository yet, create a board task to set one up; do not ask for one.
- Repo URLs, git hosting, CMS logins, deploy credentials, or "who is your dev team / who can we contact" — the platform holds the access and the agent team IS the dev team.
- Anything already visible on the board (existing tasks, projects, assignees).

## Worked Example

Request: "Add reporting."
- ❌ "Should we use a materialized view or cache?" — technical → analiz, not the stakeholder.
- ❌ "What's your SQL background?" — forbidden.
- ✅ "Which single report unblocks you first: (a) tasks per project, (b) throughput over time, (c) load per assignee?" — one product question that lets you create the first task.

Request: "Refresh the Acme website."
- ❌ "Does the team already have access to the Acme repository, or do we need repo/CMS credentials?" — `list_repositories` answers this. Call it.
- ✅ `list_repositories` → repo `acme-web` exists → proceed. Not listed → create a task to register the repo, then ask only the product question that is still open.

## Common Mistakes

- A technical question dressed as a product one.
- Asking what a read tool already knows (repo access, existing projects, board state).
- 5 questions when 1 unblocks the work.
- Blocking ALL work on a question when some tasks are already clear.
- Re-asking after you have the answer.

## Red Flags

- More than 3 questions, or any non-product question.
- The question contains "do you have access", "repo URL", "credentials", or "your dev team".
- Question posted as chat text instead of `ask_user`.
