# TaskTrooper vs Claude Squad

*Session manager · [github.com/smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad) · License: AGPL-3.0*

A terminal UI that runs many agent sessions in tmux and git worktrees.

Claude Squad manages sessions, not tasks: each instance gets a tmux session and a worktree, and you switch between them, review diffs and push. It is a good way to run several Claude Code or Codex sessions at once. TaskTrooper is a level above: tasks with owners, a lifecycle and gates, with the sessions started and resumed for you.

| | Claude Squad | TaskTrooper |
|---|---|---|
| **Where it runs** | Your terminal. | On your Mac: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted. |
| **Who starts the work** | You, per session. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Unit of work** | A session in a worktree. | A task on a board, with a branch, a PR and a column owner. |
| **Roles, lifecycle, QA, deploy** | None. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **Agents** | Claude Code, Codex, Gemini, Aider. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API. |
| **Interface** | Terminal UI. | Desktop app. |
| **License** | AGPL-3.0. | Apache-2.0. |

## Choose Claude Squad if

- You want several interactive sessions side by side in a terminal.
- You want to stay hands-on with each one.

## Choose TaskTrooper if

- You want tasks, not sessions, and you want them to move on their own.
- You want review, QA and deploy in the loop.

---

Claude Squad is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/claude-squad](https://tasktrooper.ai/compare/claude-squad).
