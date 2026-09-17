# TaskTrooper vs Beadhive and Gas City

*Software factory · [beadhive.ai](https://beadhive.ai) · License: See repositories*

A CLI software factory on top of Beads, with role agents and human gates.

Beadhive (bh) puts planner, dispatcher, developer, reviewer, merger and warden agents on top of the Beads graph, with humans holding the gates; Gas City is the orchestration platform that runs it from configuration. Both are local-first and terminal-driven. TaskTrooper shares the roles-and-gates idea and delivers it as a desktop app with a board, a QA stage that executes, and the deploy watched after the merge.

| | Beadhive and Gas City | TaskTrooper |
|---|---|---|
| **Where it runs** | Your machine, no server; a CLI. | On your Mac: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted. |
| **Who starts the work** | Planner and dispatcher agents, behind gates a human holds by default. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | Planner, dispatcher, developer, reviewer, merger, warden. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. |
| **Lifecycle** | Plan, review, merge; merge waits while a gate is open. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. |
| **QA** | Reviewer agents. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | Release automation. | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases. |
| **Interface** | CLI. | Desktop app. |
| **License** | See the repositories. | Apache-2.0. |

## Choose Beadhive and Gas City if

- You live in the terminal and want the factory configured, not clicked.
- You already use Beads and want roles on top of it.
- You want signed changes per agent identity.

## Choose TaskTrooper if

- You want a board to look at and drag cards on.
- You want QA to boot the app and drive a browser or a simulator.
- You want incidents, rollback and store releases in the same loop.

---

Beadhive and Gas City is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/beadhive](https://tasktrooper.ai/compare/beadhive).
