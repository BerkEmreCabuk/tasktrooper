# TaskTrooper vs OpenHands

*Open-source agent platform · [github.com/OpenHands/OpenHands](https://github.com/OpenHands/OpenHands) · License: MIT*

A self-hosted control center for coding agents and automations.

OpenHands is the open-source platform closest in ambition: a web frontend over one or more agent servers, Docker sandboxes, any model, and automations from Slack, GitHub, Linear and Notion. It is a general agent runner you host. TaskTrooper is narrower and more opinionated: a desktop app, a fixed delivery lifecycle, role agents that own columns, and QA and deploy stages that are part of the product rather than something you script.

| | OpenHands | TaskTrooper |
|---|---|---|
| **Where it runs** | Local, Docker sandbox, remote VMs, or OpenHands Cloud. | On your Mac: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted. |
| **Who starts the work** | You, or an automation server on a schedule or webhook. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | Agents (OpenHands, Claude Code, Codex, Gemini via ACP), configured per profile; no role model. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. |
| **Lifecycle** | Conversation and task list; GitHub issues become tasks. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. |
| **QA** | Whatever the agent runs. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | None built in; automations can be scripted. | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases. |
| **Models** | Any LLM through profiles. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API. |
| **Interface** | Web UI you host. | Desktop app for macOS, Windows and Linux. |
| **License** | MIT. | Apache-2.0. |

## Choose OpenHands if

- You want a general agent runner for more than software delivery.
- You want to host it for a team on a server.
- You want to compose your own workflow from agents and automations.

## Choose TaskTrooper if

- You want the delivery workflow already built: board, owners, review, QA, merge, deploy.
- One person, one machine, nothing to host.
- You want agents to improve their own skills from results.

---

OpenHands is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/openhands](https://tasktrooper.ai/compare/openhands).
