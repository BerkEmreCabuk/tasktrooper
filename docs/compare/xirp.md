# TaskTrooper vs Xirp

*Agentic development environment · [xirp.spotify.com/](https://xirp.spotify.com/) · License: Proprietary*

An agent grounded in your organisation's services, docs and architecture decisions.

Xirp attacks a different problem: agents that write correct code but make the wrong operational call because they lack institutional context. It connects to your services, ownership and design rationale, keeps living documentation, and holds work items and sessions in a Portal workspace. It is for teams with many microservices. TaskTrooper is for the delivery loop on one machine, and grounds agents in the repository itself: a parsed and embedded codebase, a per-repo profile, and agent memories.

| | Xirp | TaskTrooper |
|---|---|---|
| **Where it runs** | Desktop app plus a Portal plugin; beta. | On your Mac: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted. |
| **Who starts the work** | You, in a coding session. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Context** | Organisational: service ownership, dependencies, design rationale, auto-captured docs. | Repository: tree-sitter parse, local embeddings, a derived project profile, memories per agent and per repo. |
| **Roles** | One agent. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. |
| **Parallel agents and tools** | A coding session with organisational context; one agent kind. | Several tasks run at once (three sessions by default), each in its own workspace, branch and CLI session. Each role has its own tool policy, so the backend agent runs the test suite while QA drives a browser against another build and the architect reads a third task's pull request. QA has no code tools; the architect reviews and does not implement. |
| **Lifecycle** | Work items and sessions in a workspace; delivery is your existing pipeline. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. |
| **QA** | Not part of the product. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | Not part of the product. | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases. |
| **Who it is for** | Teams running many services who need agents to know the organisation. | One person shipping one or a few repositories end to end. |

## Choose Xirp if

- You have dozens of services and the hard part is knowing which one owns what.
- You are inside an organisation with a developer portal.
- You want documentation to write itself from sessions.

## Choose TaskTrooper if

- You are a team of one and the hard part is getting tasks done without driving each one.
- You want a board with owners and gates rather than a session workspace.
- You want it free and local.

---

Xirp is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/xirp](https://tasktrooper.ai/compare/xirp).
