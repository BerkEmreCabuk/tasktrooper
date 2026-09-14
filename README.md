# TaskTrooper

A local-first agent platform for software teams of one. A board of tasks, a set
of role agents (product manager, architect, backend, frontend, QA), and a
runtime that hands each task to Claude Code (or another agent CLI) on your own
machine — clone, plan, implement, test, open the PR — while you watch the run.

Everything runs on your Mac: the desktop app starts an embedded Postgres and the
Go backend, serves the UI, and runs the agent sessions locally. No account, no
cloud, no login.

## Install

Download the latest `.dmg` from Releases, drag TaskTrooper to Applications, open
it. First launch downloads two things into the app's data directory: the
Postgres binaries (~30 MB) and the embedding model (~140 MB).

You need `git` and the `claude` CLI signed in to a plan that includes Claude
Code. The app checks both and shows the exact command if one is missing.

## Run from source

```sh
make setup      # go mod download + npm ci (desktop, desktop/ui)
make desktop    # Electron app in dev mode
make dev        # or: backend + UI dev server, open http://localhost:3200
make package    # build the .dmg into desktop/release
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

## License

Apache-2.0. See `LICENSE`.
