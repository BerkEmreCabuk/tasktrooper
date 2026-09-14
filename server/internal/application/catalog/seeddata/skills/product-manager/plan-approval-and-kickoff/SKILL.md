---
name: plan-approval-and-kickoff
category: pm
description: Present plan to stakeholder for approval, then move approved tasks to todo
---

# Plan Approval and Kickoff

After creating implementation tasks in backlog:

1. Call ask_user with a concise plan summary:
   - List each task: title, assignee, type, brief rationale.
   - Ask: "Approve to start all tasks, or select which ones to start now?"
2. When stakeholder approves:
   - Determine start order respecting dependencies:
     * If analiz tasks exist and are already running: keep implementation tasks in backlog until analiz done.
     * If no analiz dependency: move tasks to todo in logical order (backend before frontend if frontend depends on API).
   - Call move_board_task for each approved task: column=todo.
   - Report: which tasks were moved to todo (agents will pick up), which stay in backlog and why.
3. If stakeholder approves partially: only move selected tasks to todo, leave rest in backlog.
4. If stakeholder rejects or wants changes: update_board_task or delete and recreate with corrected details.
