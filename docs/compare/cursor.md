# TaskTrooper vs Cursor Cloud Agents

*Hosted coding agents · [cursor.com/docs/cloud-agent](https://cursor.com/docs/cloud-agent) · License: Proprietary*

Parallel agents in cloud VMs that open merge-ready PRs.

Cursor's cloud agents run in isolated VMs, start from the web, Slack, Linear or a PR comment, and hand back a PR with screenshots and logs. They are strong at the implement-and-open-PR step. TaskTrooper covers the steps around it, on your own machine, and can run the Cursor CLI as one of its agent runtimes.

| | Cursor Cloud Agents | TaskTrooper |
|---|---|---|
| **Where it runs** | Isolated VMs in Cursor's cloud; your repo is cloned there. | On your Mac: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted. |
| **Who starts the work** | You, per agent, from Cursor, Slack, GitHub or Linear. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | One agent kind. Instructions come from rules files. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. |
| **Parallel agents and tools** | As many cloud agents as you start, each with the same capabilities. | Several tasks run at once (three sessions by default), each in its own workspace, branch and CLI session. Each role has its own tool policy, so the backend agent runs the test suite while QA drives a browser against another build and the architect reads a third task's pull request. QA has no code tools; the architect reviews and does not implement. |
| **Lifecycle** | Task in, PR out. Review is your PR flow. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. |
| **QA** | The agent verifies its own work and attaches screenshots and logs. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. A separate agent from the one that wrote the code. |
| **After merge** | None. | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases. |
| **Models** | Cursor's model list, billed at API rates with a spend limit you set. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API. |
| **Data** | Code and context leave your machine. | Stays on the machine. One bearer token between the app and its own loopback server. |
| **Price** | Cursor plan plus per-agent model usage. | Free, Apache-2.0. You pay your own model or CLI subscription. |

## Choose Cursor Cloud Agents if

- You want to fire off agents from Slack and your laptop can be closed.
- Your team is already in Cursor and its PR flow is enough.
- You prefer a vendor-run environment to a local one.

## Choose TaskTrooper if

- The code must not leave your machine.
- You want owners per stage, not one agent per prompt.
- You want QA, merge and deploy inside the same loop as the implementation.

## Together?

Yes: TaskTrooper can run the Cursor CLI as a local agent runtime.

---

Cursor Cloud Agents is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/cursor](https://tasktrooper.ai/compare/cursor).
