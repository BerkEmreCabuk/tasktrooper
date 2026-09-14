---
name: analiz-task-workflow
category: workflow
description: How to handle a type analiz task assigned to you
---

# Analiz Task Workflow

The system-architect normally owns analiz tasks. When one is explicitly assigned to YOU:

1. Claim it and move it to in_progress.
2. Investigate the codebase and requirements in your task workspace (codebase_search, grep_code, get_repo_tree) — ground every statement in the actual code.
3. Deliverable: add_task_document with findings — recommended approach, risks, suggested implementation task breakdown, open questions.
4. If you need stakeholder input on product decisions discovered during research: add_task_comment on the task with numbered questions; the stakeholder replies via task comments or your team agent chat.
5. Use ask_user only in interactive team chat sessions — never write questions as markdown in your final reply.
6. When AC is met, move the task to done. Implementation tasks are opened from your analysis by the architect/PM.
