# How TaskTrooper compares

Generated from the site's `content/compare.ts` and `content/matrix.ts` with `node scripts/export-compare.ts`; edit there, not here.

### Side by side

Four tools that also put coding agents to work for you. They stop at the pull request or the commit; TaskTrooper carries the task through QA, merge, deploy and production, and the board starts the work.

| | [TaskTrooper](https://github.com/makifbaysal/tasktrooper) | [Orca](https://www.onorca.dev) | [bb](https://getbb.app) | [Modula](https://modula.sh) | [Teleclod](https://teleclod.com) |
|---|---|---|---|---|---|
| What it is | Desktop app: board, role agents and the runtime that runs them | IDE that runs many agent CLIs side by side, one git worktree per task | Agent IDE built from plugins; desktop, web, CLI and API | Event-driven pipeline of agents that turns tickets into code | Dashboard for Claude Code and Codex sessions, run from your phone |
| Who starts the work | The board: a card entering a column is dispatched to that column's agent | You, per worktree; experimental agent-to-agent orchestration | You, per thread, or scripts and cron | Agent rules that match events, after a human approves the task | You describe the task; an agent plans it and waits for approval |
| Roles | Seven seeded agents (PM, architect, backend, frontend, mobile, QA, release engineer) with skills and rules that rewrite themselves from results | None | None; manager threads coordinate others | Project manager, researcher, worker, code reviewer, reviewer, plus Jira/Linear/GitHub scan agents | One agent per task; a different model per phase |
| Isolation | Local clone and branch per task | Git worktree per task | Worktree, live checkout, scratch folder or Modal sandbox | Git worktree per solution variant | Not described |
| Review | Pull request per task; every acceptance criterion needs a verdict | In-app diff; line comments go back to the agent as one prompt | Diff panel with path filters | Reviewer agents, then an in-app diff for the human | Findings against your rules by severity; you approve before commit |
| QA | A QA agent that cannot pass a task without running something: real requests, headless browser, iOS/Android simulators | Whatever the agent runs; embedded browser | None built in | None | Review against your rules; agents can check your Chrome tabs |
| After merge | Deploy recipes, production checks, rollback, incidents from Alertmanager/Sentry/webhooks, App Store and Play releases | None | None | None | None |
| Agent runtimes | Claude Code, Cursor, Antigravity, OpenCode as local processes; OpenAI, Anthropic, Gemini, Groq or any OpenAI-compatible API | 30+ agent CLIs | Claude Code, Codex, Cursor, OpenCode, Grok, Pi and other ACP agents | Claude Code, Codex, OpenCode, Gemini CLI | Claude Code, Codex, Gemini, Kimi CLI and API providers |
| Remote access | None; nothing hosted | SSH hosts, remote server, iOS/Android through a relay | Several machines, bb connect, Tailscale, iOS | Closed-source plugin | Phone, browser and Chrome through a relay; Docker |
| Storage | Embedded Postgres in the app's data directory | SQLite | SQLite | SQLite under `~/.modula` | Encrypted SQLite in the project folder |
| Interface | Desktop app (macOS, Windows, Linux) | Desktop app plus mobile apps | Desktop, web, CLI, iOS | Desktop app and CLI | Desktop app, phone, Chrome extension |
| License and price | Apache-2.0, free | MIT, free | MIT, free | Elastic License 2.0, free | Proprietary; free tier, €49 and €99 per month |

Per-project write-ups, same content as [tasktrooper.ai/compare](https://tasktrooper.ai/compare):

- [TaskTrooper vs Orca](orca.md) — An IDE that runs many coding agents side by side, each in its own git worktree.
- [TaskTrooper vs bb](bb.md) — An agent IDE built from plugins, driven from a desktop app, the web, a CLI or an API.
- [TaskTrooper vs Modula](modula.md) — An event-driven pipeline that turns tickets into code with a team of agents.
- [TaskTrooper vs Teleclod](teleclod.md) — A dashboard for many Claude Code and Codex sessions that you run from your phone.
