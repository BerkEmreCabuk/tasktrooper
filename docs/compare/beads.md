# TaskTrooper vs Beads

*Agent issue tracker · [github.com/steveyegge/beads](https://github.com/steveyegge/beads) · License: MIT*

A git-embedded, dependency-aware issue tracker and memory for coding agents.

Beads tracks work for agents; it does not run them. Issues live in Dolt under .beads/, sync through git, carry typed dependencies, and bd ready answers what is unblocked. TaskTrooper keeps the same kind of graph in its own Postgres, and adds the part Beads leaves to you: the agents, the board that dispatches them, review and QA, and the deploy.

| | Beads | TaskTrooper |
|---|---|---|
| **What it is** | A CLI (bd) and a database; agents call it from any tool. | A desktop app that runs the agents and owns the tracker. |
| **Who starts the work** | Whatever agent you are already running. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Work graph** | blocks, parent-child, discovered-from, related; bd ready. | blocked_by, deploy_depends_on, derived_from, discovered_from; a ready queue tool; blockers park the card until they land. |
| **Storage** | Dolt in .beads/, pushed and pulled through the git remote. | Embedded Postgres in the app's data directory. |
| **Roles, lifecycle, QA, deploy** | Out of scope by design. | Six role agents (PM, architect, backend, frontend, mobile, QA) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **Parallel agents and tools** | Any number of agents can share the graph; what each may do is up to the agent you run. | Several tasks run at once (three sessions by default), each in its own workspace, branch and CLI session. Each role has its own tool policy, so the backend agent runs the test suite while QA drives a browser against another build and the architect reads a third task's pull request. QA has no code tools; the architect reviews and does not implement. |
| **Interface** | CLI; community UIs. | Desktop app. |
| **License** | MIT. | Apache-2.0. |

## Choose Beads if

- You want a tracker your agents call from any tool, versioned with the repo.
- You already run your own agents and only need memory and ordering.
- You want the data in git.

## Choose TaskTrooper if

- You want the agents run for you, not just tracked.
- You want review, QA and deploy stages with owners.
- You want a board you can look at.

---

Beads is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/beads](https://tasktrooper.ai/compare/beads).
