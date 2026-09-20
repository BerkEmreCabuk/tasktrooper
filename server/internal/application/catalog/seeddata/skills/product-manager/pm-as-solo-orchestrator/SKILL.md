---
name: pm-as-solo-orchestrator
category: pm
description: How PM behaves as the sole orchestration agent
---

# PM as Solo Orchestrator

When product-manager is the only enabled orchestration agent:

- All subtasks must be assigned to product-manager — never to system-architect, backend-developer, frontend-developer, mobile-developer, devops-engineer, or qa-agent.
- Developer work goes through create_board_task, not orchestration subtasks.
- Multiple parallel PM subtasks are fine only when every one of them is read-only (e.g. web research + list_board_tasks concurrently). At most one subtask per parallel group may write to the board; a second board writer in the same group rejects the whole plan. Use depends_on to sequence when a later subtask needs prior results.
- Planner sets ready=false only when a product decision blocks creating any board task (max 3 questions). Otherwise ready=true.
- Standard flow: create tasks in backlog → call ask_user for stakeholder approval → move approved tasks to todo. Use the plan-approval-and-kickoff skill for this. This is one subtask calling tools in order, NOT three subtasks — splitting it hands the same board record to several agents that cannot see each other.
- Delivery stages (analiz, implementation, QA verification, pm_uat, approval) are board columns the task travels through afterwards. Never plan a subtask per stage; that opens one board record per stage for a single piece of work.
- Analiz tasks skip approval — they go directly to todo (investigation is always safe to start).
- If a blocking product decision remains, call ask_user within a subtask — never append questions as markdown in the final summary.
