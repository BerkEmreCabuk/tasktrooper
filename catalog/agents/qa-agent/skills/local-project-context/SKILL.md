---
name: local-project-context
category: tools
description: Use when running any task in this system - explains the local (non-CI) execution environment, the task workspace, and the exploration/verification tools available
---
# Local Project Context

## Overview

You run on a local developer machine, not in a CI/CD pipeline. There is no remote runner that will catch your mistakes later — the build and tests you run in your workspace are the only verification that happens before a human sees the result. Treat your workspace as production's last line of defense.

**Core principle:** Explore before you change; verify in this environment before you claim.

## The environment

- **Task workspace:** each task is worked in a cloned checkout on its own branch. Your working directory is injected into the session context — always use it as the root, never assume paths from another task.
- **Live codebase:** your changes take effect immediately in the workspace. There is no separate deploy step for verification.
- **No inherited state:** never assume a prior session left a build artifact, a running server, or a seeded database. Build, migrate, and test from scratch every time.

## Exploration tools (use before changing code)

| Tool | Use for |
|------|---------|
| `codebase_search` | Concept/semantic search — "where is auth handled" |
| `grep_code` | Exact symbol/string — a function name, an error message |
| `get_repo_tree` | Structure and file layout of the repository |
| `expand_symbol_context` | Focused read of a symbol and its surroundings |
| `read_file` | Read a file with line numbers — up to 800 lines per call |
| `run_terminal` | Build, test, run the app, inspect output |

## File tools (use instead of the shell for anything touching a file)

| Tool | Use for |
|------|---------|
| `read_file` | Read with line numbers, whole file in one call |
| `edit_file` | Replace an exact string; `replace_all: true` changes every occurrence at once |
| `edit_lines` | Replace / insert / delete by line number — add a function, an import, remove a block |
| `write_file` | Create a file, or replace one whole |
| `delete_file` | Remove a file or directory |
| `move_file` | Rename or move |

Never `cat`, `sed -n`, `head`, `tail`, `sed -i`, `rm`, `mv` or a heredoc through the shell. A shell read costs a full agent turn per window; a `sed -i` is silent about what it matched, so you spend a second turn grepping to find out whether it worked; and a heredoc mangles backticks, quotes and template literals.

Every file tool reports what it did — the number of occurrences changed, the line numbers, the region as it now reads. **That is your confirmation. Do not grep or re-read the file to check.** One edit is one turn.

Find the existing pattern first (the neighboring endpoint, the similar component, the analogous test) and follow it. A parallel convention you invent is a review finding waiting to happen.

## Verification (use before every handoff)

Run the real command in this run and read the output — see verify-before-done.

| Stack | Build check | Test check |
|-------|-------------|------------|
| Go backend | `go build ./...` | `go test ./internal/...` (affected packages) |
| Java backend | `./mvnw -q package` (or `quarkus build`) | `./mvnw test` |
| Web frontend | `npm run build` (in `web/`) | `npm test` |
| Flutter mobile | `flutter build` | `flutter test` |

If the build or tests do not pass in this run, the task is not ready to move forward — say what failed.

## Common Mistakes

- Reasoning about behavior from reading code instead of running it.
- Reusing a path or branch name from a previous task's workspace.
- Assuming `npm install` / `go mod download` already ran — check, don't assume.

## Red Flags

- "It built last time" — build again, now.
- "The server should still be running" — never assume; start it.
