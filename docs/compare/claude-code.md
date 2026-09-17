# TaskTrooper vs Claude Code

*Agent CLI · [claude.com/claude-code](https://claude.com/claude-code) · License: Proprietary*

The terminal agent TaskTrooper runs underneath.

Claude Code is the engine, not the competitor: TaskTrooper starts a headless Claude Code session per task and hands it the board over MCP. On its own, Claude Code is one session that you drive, prompt by prompt. TaskTrooper is what turns many of those sessions into a team with a board, owners, review gates and a deploy step.

| | Claude Code | TaskTrooper |
|---|---|---|
| **Who starts the work** | You, in a terminal, one prompt at a time. Subagents and hooks exist, but a human opens every session. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | One agent per session. Role prompts and skills are yours to write and maintain. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. |
| **Parallel agents and tools** | One session, one set of tools. Parallelism is you opening more terminals; every session can do everything you allow. | Several tasks run at once (three sessions by default), each in its own workspace, branch and CLI session. Each role has its own tool policy, so the backend agent runs the test suite while QA drives a browser against another build and the architect reads a third task's pull request. QA has no code tools; the architect reviews and does not implement. |
| **Lifecycle** | None. The session ends when the model stops; what happens next is up to you. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. |
| **QA** | Whatever you ask it to run in that session. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **Usage limits** | An interactive session now waits and continues automatically at the reset time (esc to cancel). That covers the one terminal you keep open; a headless run or a second task gets nothing. | Every running task parks on Blocked with its resume time, other runs are held instead of hitting the same wall, and each session resumes with --resume when the limit resets, keeping what it already read and wrote. Unattended, across the whole board. |
| **After merge** | None. | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases. |
| **Memory** | CLAUDE.md files and session history. | Per-agent memories in four scopes, versioned skills and rules, plus CLAUDE.md is still read by the session. |
| **Price** | Claude subscription or API usage. | Free, Apache-2.0. You pay your own model or CLI subscription. Claude Code's own subscription still applies. |

## Choose Claude Code if

- You want one fast interactive session for the thing in front of you.
- Your workflow is a terminal and a PR, no board.
- You do not want another app running.

## Choose TaskTrooper if

- You have more tasks than sessions you can babysit.
- You want review, QA and deploy to happen without you queuing them.
- You want a usage limit handled for every running task, headless, not only the terminal you are watching.

## Together?

Always. TaskTrooper needs Claude Code (or another agent CLI) installed and never replaces it.

---

Claude Code is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/claude-code](https://tasktrooper.ai/compare/claude-code).
