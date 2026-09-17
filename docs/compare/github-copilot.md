# TaskTrooper vs GitHub Copilot coding agent

*Hosted coding agent · [docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) · License: Proprietary*

Assign an issue to Copilot and get a PR from a GitHub Actions runner.

Copilot's coding agent lives inside GitHub: assign it an issue or mention it on a PR, it works in an ephemeral Actions environment and opens a pull request. One repo, one PR, a 59-minute cap, paid Copilot plans only. TaskTrooper uses GitHub for the same PR and CI, but the agents, the board and everything after the PR run on your machine.

| | GitHub Copilot coding agent | TaskTrooper |
|---|---|---|
| **Where it runs** | GitHub Actions, ephemeral per session. | On your Mac: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted. |
| **Who starts the work** | You assign an issue or mention @copilot; scheduled automations exist. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | One agent. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. |
| **Parallel agents and tools** | One session per issue, capped at 59 minutes; the same agent every time. | Several tasks run at once (three sessions by default), each in its own workspace, branch and CLI session. Each role has its own tool policy, so the backend agent runs the test suite while QA drives a browser against another build and the architect reads a third task's pull request. QA has no code tools; the architect reviews and does not implement. |
| **Lifecycle** | Issue to PR. Review is GitHub's PR flow. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. GitHub PRs and Actions status are read into it. |
| **QA** | Your CI on the PR. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **Limits** | GitHub-hosted repos only, one repo and one PR per task, 59 minutes per session. | Any git remote your machine can clone; a task runs as long as the CLI session does, and parks instead of failing on a usage limit. |
| **After merge** | None. | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases. |
| **Price** | Paid Copilot plans; premium requests. | Free, Apache-2.0. You pay your own model or CLI subscription. |

## Choose GitHub Copilot coding agent if

- Everything already happens in GitHub issues and you want zero setup.
- Small, bounded issues that fit in an hour.
- Your organisation has Copilot and nothing else is allowed.

## Choose TaskTrooper if

- Multi-step work with analysis, implementation, review and QA as separate stages.
- Tasks that need a real environment: a database, a browser, a simulator.
- You want the deploy watched after the merge.

## Together?

Yes: TaskTrooper reads the PR and Actions status Copilot's PRs produce, but it will not dispatch to Copilot.

---

GitHub Copilot coding agent is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/github-copilot](https://tasktrooper.ai/compare/github-copilot).
