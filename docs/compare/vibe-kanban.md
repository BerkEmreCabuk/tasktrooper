# TaskTrooper vs Vibe Kanban

*Kanban for coding agents · [github.com/BloopAI/vibe-kanban](https://github.com/BloopAI/vibe-kanban) · License: Apache-2.0*

A kanban board where each task gets a branch, a terminal and a dev server.

Vibe Kanban gives coding agents a board and an isolated workspace per task, and supports a long list of agents. You start each task and review the result; the project has announced it is sunsetting. TaskTrooper's board dispatches by itself, gives each column an owner, and carries the task through review, QA, merge and deploy.

| | Vibe Kanban | TaskTrooper |
|---|---|---|
| **Where it runs** | Local via npx, or a self-hosted server. | On your Mac: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted. |
| **Who starts the work** | You, per task. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | None; pick an agent per task. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. |
| **Lifecycle** | Todo, in progress, review. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. |
| **QA** | None. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | None. | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases. |
| **Agents** | Claude Code, Codex, Gemini CLI, Copilot, Amp, Cursor, OpenCode and more. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API. |
| **Status** | Announced as sunsetting. | Active. |
| **License** | Apache-2.0. | Apache-2.0. |

## Choose Vibe Kanban if

- You want the widest agent list and a light board.
- You want to start and watch each task yourself.

## Choose TaskTrooper if

- You want the board to dispatch and the task to keep moving.
- You want QA and deploy stages, not only review.
- You want something maintained.

---

Vibe Kanban is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/vibe-kanban](https://tasktrooper.ai/compare/vibe-kanban).
