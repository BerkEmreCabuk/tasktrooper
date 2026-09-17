<p align="center">
  <a href="https://tasktrooper.ai"><img src="docs/assets/banner.png" alt="TaskTrooper — Put it on the board. The agents ship it." width="100%"></a>
</p>

<p align="center">
  <a href="https://tasktrooper.ai"><img src="https://img.shields.io/badge/web-tasktrooper.ai-f0b86e" alt="tasktrooper.ai"></a>
  <a href="https://github.com/makifbaysal/tasktrooper/releases"><img src="https://img.shields.io/github/v/release/makifbaysal/tasktrooper?label=release&color=6a2d68" alt="release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue" alt="license"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey" alt="platform">
  <img src="https://img.shields.io/badge/backend-Go-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/desktop-Electron-47848F?logo=electron&logoColor=white" alt="Electron">
  <a href="#install"><img src="https://img.shields.io/badge/brew-makifbaysal%2Ftasktrooper-fbb040?logo=homebrew&logoColor=white" alt="Homebrew"></a>
</p>

# TaskTrooper

**Website:** [tasktrooper.ai](https://tasktrooper.ai) · **Download:** [Releases](https://github.com/makifbaysal/tasktrooper/releases)

A local-first agent platform for software teams of one. A board of tasks, a set
of role agents (product manager, architect, backend, frontend, QA), and a
runtime that hands each task to Claude Code (or another agent CLI) on your own
machine — clone, plan, implement, test, open the PR — while you watch the run.

Everything runs on your Mac: the desktop app starts an embedded Postgres and the
Go backend, serves the UI, and runs the agent sessions locally. No account, no
cloud, no login.

**You don't drive the agents, the board does.** Put a task on the board and the
agent that owns its column picks it up on its own, does the work and hands the
task on to the next column, where the next agent takes over. Nobody has to press
run.

**A usage limit doesn't lose work.** When Claude Code runs out of its usage
limit in the middle of a task, the agent stops and the task waits on Blocked.
Once the limit resets, TaskTrooper resumes the same Claude Code session, so the
agent carries on from where it stopped instead of starting over.

## Features

### Board

- A Kanban board with thirteen columns out of the box, from Backlog to Released,
  including analysis review, code review, QA, PM UAT and human UAT. The columns,
  and which agents pick up work in each, are configurable.
- Agents take tasks by themselves. A task that lands in a column is dispatched
  to that column's agent automatically and moves on when the agent is done.
- Tasks carry acceptance criteria. A task cannot move forward out of a review
  column until every criterion has a verdict.
- Tasks can block each other; a blocked task waits until its blocker is done.
- Every task gets its own branch and pull request. Code review reads the PR,
  Done merges it, and Done and Released watch the deploy and can roll it back.
- When Claude Code hits its usage limit, the task is parked on Blocked until the
  limit resets. Then it resumes by itself with `claude --resume` on the parked
  session, so the agent keeps what it already read and wrote and continues from
  where it stopped.

### Role agents

Six agents ship with a library of 95 seeded skills plus rules. They run on
Claude Code with `sonnet` by default and `opus` for subtasks rated hard.

| Agent | Works on |
|---|---|
| `product-manager` | backlog, requirements, PM UAT |
| `system-architect` | analysis tasks, task breakdown, code review |
| `backend-developer` | APIs, databases and tests (Go, Java/Quarkus) |
| `frontend-developer` | React, Vite and Tailwind UIs |
| `mobile-developer` | Flutter, SwiftUI, Compose, store releases |
| `qa-agent` | manual test rounds with real requests and headless-browser screenshots; it cannot pass a task without running something |

Every agent is editable: provider and model, tool policy, effort, skills, rules,
the columns it works, and its memory. You can chat with any agent directly, or
about a specific task.

### Self-evolution

Agents rewrite their own playbooks from how their work actually went.

- **Reflection.** On a schedule, whenever a task is sent back to Need Revision,
  or on demand, an agent reviews everything since its last reflection: its
  runs, chat messages, revision comments, scores, KPI results and its current
  skills, rules and memories. It proposes changes to all three. Skill and rule
  changes are applied only for agents with self-evolution turned on.
- **Golden gate.** With the gate enabled, a golden task suite runs before and
  after a proposed change, and an independent judge model decides whether to
  keep it. If the pass rate drops, the whole change set is reverted
  automatically.
- **Impact tracking.** Each applied change is later classified as effective,
  regressed or neutral by comparing scores before and after it. Regressions are
  put in front of the agent's next reflection, which decides whether to revert.
- **Budgets and history.** Skills and rules are capped per agent (25 and 15 by
  default), so an agent merges and updates instead of piling up. Every write to
  a skill or rule is versioned with its source (you, self-evolution or the seed)
  and any version can be restored.
- **KPIs.** Each agent has targets such as tasks completed, first-pass rate,
  revisions received, UAT failures, failed runs and time spent per column,
  measured per day, week or month. The targets are part of the agent's prompt,
  and the Performance page shows how it is doing.

### Memory and code understanding

- Agents save and search memories in four scopes: personal or team-wide, for one
  repository or for every repository. Notes about a single run are refused, and
  a near-duplicate is not saved twice.
- Repositories are parsed with tree-sitter and embedded on your machine with the
  bundled `nomic-embed-text-v1.5` model, so agents search code semantically and
  see uncommitted edits without a re-index.
- Skills are loaded on demand, so a long skill list does not fill the prompt.
- Files uploaded to a workspace are available to agents through retrieval.

### Runtimes, models and tools

- Agent CLIs run as local processes: Claude Code, Cursor, Antigravity and
  OpenCode. A Claude Code session gets TaskTrooper's board tools over MCP with a
  per-run token, and never loads a repository's `.mcp.json` or your personal
  Claude Code settings.
- API providers for agents that do not use a CLI: OpenAI, Anthropic, Google
  Gemini, Groq, or any OpenAI-compatible endpoint such as LM Studio, Ollama or
  vLLM.
- Connect your own MCP servers.
- Built-in tools: terminal, file editing, web search with no API key, page
  fetch, headless-browser QA, and a boilerplate catalog to start new projects
  from.
- A repository's own version pins (`.tool-versions`, `go.mod`, `.nvmrc` and
  others) are honoured in the agent session.

### Integrations and operations

- **GitHub:** clone, branches, pull requests, review comments, merge, and CI
  status from GitHub Actions.
- **Deploys:** a recipe catalog for Google Cloud Run and GKE, AWS ECS and Lambda,
  Vercel and Fly, rendered into a workflow per environment with a health check,
  plus a deployment matrix. A Vercel account can be connected to bind existing
  projects.
- **Production incidents:** alerts from Alertmanager, Sentry, Cloud Monitoring or
  any JSON webhook, together with a health monitor, fold into deduplicated
  incidents with a suggested remedy: a rollback, a config, dependency or
  capacity fix, or a code defect. Per repository you choose whether an incident
  is only recorded, becomes a diagnosis task, or is fixed through the board.
- **Mobile releases:** connect App Store Connect and Google Play and promote
  builds through internal, external and production channels. QA can drive iOS
  simulators and Android emulators on this Mac through Appium.
- **Usage:** token usage per model and per day.

### First run

A guided setup checks this Mac (git, the agent CLIs you have, and optionally
Chrome, Xcode, Appium and the Android SDK), lets you connect any of Claude Code,
Cursor, Antigravity and OpenCode, or an API provider with your own key, then
connects GitHub and imports your first project.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/makifbaysal/tasktrooper/main/scripts/install.sh | bash
```

It downloads the latest release's universal `.dmg`, copies TaskTrooper into
`/Applications`, and prints the commands for anything else you need. Pass `-y`
to replace an existing install without being asked.

With Homebrew once this repository is public; the repository is its own tap:

```sh
brew tap makifbaysal/tasktrooper https://github.com/makifbaysal/tasktrooper
brew install --cask tasktrooper
```

The app is ad-hoc signed and not notarized yet, so a copy you
install by hand may need right-click → Open the first time; the install script
and the cask both clear the quarantine flag for you.

First launch downloads two things into the app's data directory: the Postgres
binaries (~30 MB) and the embedding model (~140 MB). You need `git` and at least
one way to run agents: the Claude Code, Cursor, Antigravity or OpenCode CLI, or
an API key for a model provider. The app checks what is installed and shows the
exact command for anything missing.

Or grab the `.dmg` from [Releases](https://github.com/makifbaysal/tasktrooper/releases) and drag TaskTrooper to Applications.

On **Windows**, download `TaskTrooper-<version>-setup.exe` from Releases and run
it. On **Linux**, use `TaskTrooper-<version>-x86_64.AppImage` (`chmod +x`, then
run it) or install the `.deb`. Neither is code-signed yet, so Windows SmartScreen
asks you to confirm the first time. The install script and Homebrew are
macOS-only.

## Run from source

```sh
make setup      # go mod download + npm ci (desktop, desktop/ui)
make desktop    # Electron app in dev mode
make dev        # or: backend + UI dev server, open http://localhost:3200
make package    # build the installer for this OS into desktop/release
```

## Layout

```
server/       Go backend — API, board, agent loop, tools, embedded Postgres
desktop/      Electron shell — supervises the backend + embedder, serves the UI
desktop/ui/   React UI — bundled into the app; runs in a browser for development
docs/         short reference docs — start with docs/architecture.md
```

Each directory has its own `README.md` and `CLAUDE.md`.

## How a task runs

1. You put a card on the board (or a product-manager agent drafts it).
2. The backend prepares a git workspace under the data directory and starts a
   headless `claude -p` session with the agent's prompt, skills and a per-run
   MCP token that lets the session update acceptance criteria and move the
   card.
3. The session's output streams to the card. When it finishes, QA agents run
   the test round; CI status is polled from GitHub Actions.
4. The card moves through the columns you configured; a PR is opened on the
   task branch.

## Configuration

The desktop app needs none. For `make dev`, `scripts/dev.sh` writes
`server/.env.local` on first run; see `server/README.md` for every variable.

## How it compares

TaskTrooper sits next to a few projects that also put coding agents to work.
The short version: the others give you a tracker, a queue or a session manager
that *you* drive; TaskTrooper is the whole loop in one desktop app, and the
board drives it.

| | TaskTrooper | [Beads](https://github.com/steveyegge/beads) | [Beadhive](https://beadhive.ai) / [Gas City](https://gascity.com) | [Vibe Kanban](https://github.com/BloopAI/vibe-kanban) | [Claude Squad](https://github.com/smtg-ai/claude-squad) |
|---|---|---|---|---|---|
| What it is | Desktop app: board, role agents and the runtime that runs them | Git-embedded issue tracker and memory for agents (`bd` CLI) | Software factory on top of Beads (`bh` CLI; Gas City orchestrates it) | Kanban and per-task workspaces for coding agents (announced as sunsetting) | TUI that runs many agent sessions side by side |
| Who starts the work | The board: a card entering a column is dispatched to that column's agent | You, or an agent you are already running | Its planner/dispatcher agents, behind human gates | You, per task | You, per session |
| Roles | Six seeded agents (PM, architect, backend, frontend, mobile, QA) with skills and rules that rewrite themselves from results | None, it tracks work for any agent | Planner, dispatcher, developer, reviewer, merger, warden | None | None |
| Lifecycle | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy | Open/closed with typed dependencies | Plan, review, merge with human gates | Todo, in progress, review | Branch, diff, commit |
| QA | A QA agent that cannot pass a task without running something: real requests, headless browser, iOS/Android simulators | None | Reviewer agents | None | None |
| After merge | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases | None | Release automation | None | None |
| Work ordering | `blocked_by`, `deploy_depends_on`, `derived_from`, `discovered_from`; a ready queue for agents | `blocks`, `parent-child`, `discovered-from`, `related`; `bd ready` | Beads' graph | None | None |
| Storage | Embedded Postgres in the app's data directory | Dolt under `.beads/`, synced through git | Beads | Local database, or a self-hosted server | tmux sessions and git worktrees |
| Agent runtimes | Claude Code, Cursor, Antigravity, OpenCode as local processes; OpenAI, Anthropic, Gemini, Groq or any OpenAI-compatible API | Any agent that can call a CLI | Any, through its CLI | Claude Code, Codex, Gemini CLI, Copilot, Amp, Cursor, OpenCode and more | Claude Code, Codex, Gemini, Aider |
| Interface | Desktop app (macOS, Windows, Linux) | CLI, plus community UIs | CLI | Web UI | Terminal UI |
| License | Apache-2.0 | MIT | See their repositories | Apache-2.0 | AGPL-3.0 |

Per-project write-ups, including Claude Code on its own, Cursor, Devin, GitHub
Copilot's coding agent, OpenHands and Spotify's Xirp, are under
[`docs/compare/`](docs/compare/README.md) and on
[tasktrooper.ai/compare](https://tasktrooper.ai/compare).

## License

Apache-2.0. See `LICENSE`.
