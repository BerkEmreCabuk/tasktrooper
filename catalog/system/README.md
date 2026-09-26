# catalog/system

Every piece of LLM-facing prose the server renders — orchestrator prompts,
prompt-builder text, guard wording, tool descriptions — as data, loaded by
`server/internal/application/prompt` (see that package's doc comment for the
Go-side API). Code passes data in; nothing here is a Go string literal.

This directory is embedded into the server binary at build time
(`catalog/embed.go`, `catalog.SystemFS()`) so a prompt is available the
instant the process starts — the DB sync of `catalog/agents/**` runs async
after boot and prompts must never depend on it. An operator can still point
`AGENT_CATALOG_REPO` at a local directory carrying its own `system/` tree to
override the embedded prompts at runtime without a rebuild; see
`internal/adapter/catalogrepo/promptdir.go` and the "Prompt library" section
of `server/.ai/architecture.md`.

## Layout

```
system/
  prompts/<area>/<name>.md   orchestrator/planner/board/etc. system prompts
  guards/<code>.md           guard rejection wording, keyed by the guard's code
  tools/<name>.md            tool descriptions shown to an LLM
  schemas/<name>.json         JSON schemas referenced from a prompt's front matter
  partials/<name>.md          fragments included from other files with `partial`
```

A file's path fixes its key — the front matter `key` field must equal it
exactly, or loading fails naming the file:

| Path                                    | Key                          |
|------------------------------------------|-------------------------------|
| `prompts/orchestrator/planner_system.md`  | `orchestrator.planner_system` |
| `guards/<code>.md`                        | `guard.<code>`                |
| `tools/<name>.md`                         | `tool.<name>`                 |
| `partials/<name>.md`                      | `partial.<name>`              |

A nested area (`prompts/board/dispatch/wake.md`) joins every path segment
after `prompts/` with `.`: `board.dispatch.wake`.

## Front matter

Every file (except `README.md`, `schemas/*.json`, and `.gitkeep` placeholders)
starts with a YAML front matter block:

```
---
key: orchestrator.planner_system
version: 1
inputs: [Task, Context]
schema: schemas/planner.json
---
The template body goes here.
```

- `key` (required) — must equal the path-derived key above.
- `version` (optional) — free-form; bump it when the wording changes in a
  way callers should be able to notice.
- `inputs` (optional) — names of the fields the template expects on its data
  argument. Informational only (not enforced) — a comment for the next
  person editing the file.
- `schema` (optional) — path under `schemas/` to a JSON schema describing the
  data shape, when one is worth writing down.

## Body

The body is a Go `text/template` (`Option("missingkey=error")` — a template
that reads a map key that is not there fails loudly instead of printing
`<no value>`), rendered by `prompt.Library.Render`/`MustRender`.

Helpers available in every template:

- `join sep items` — `strings.Join`.
- `bullets items` — each item on its own `- item` line.
- `trunc n s` — cut `s` to at most `n` runes; adds nothing, so a truncated
  and an untruncated string of exactly `n` runes are indistinguishable —
  callers that need to signal truncation add their own marker.
- `indent n s` — prefix every line of `s` with `n` spaces.
- `plural n singular plural` — `singular` when `n == 1`, else `plural`.
- `lower`, `upper` — `strings.ToLower`/`ToUpper`.
- `partial "name" .` — render `partials/name.md` with the current data and
  splice its output in.

**Byte-exact bodies.** A migrated prompt must render identically,
byte-for-byte, to the Go string literal it replaced. The loader is exact
about what it keeps: the body is everything after the closing `---` line,
with exactly one trailing `"\n"` stripped if the file ends with one (nothing
else — no further trimming, no whitespace collapsing). Save a body with a
single trailing newline, the way every editor does, and the loaded string
reproduces the original exactly. A body with no trailing newline in the
original must be saved with no trailing newline in the file either, or that
one newline is not there to strip and the round trip already matched.

## Partials

A `partials/*.md` file is loaded and parsed like any other template but is
never required to have a corresponding `prompt.Define[T]` key in Go — it
exists only to be pulled in with `partial "name" .` from other templates.
