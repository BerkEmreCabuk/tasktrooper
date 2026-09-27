---
key: briefs.repodocs.docs_bundle
version: 1
inputs: [RepoKind, Docs]
---
Author the reference docs listed below for this {{.RepoKind}} repository, ALL of them in a single branch so exactly one pull request contains every file.

Do not open a pull request per document, and do not stop after the first one — the task is finished when every path below exists and is correct.

{{range .Docs}}{{.Number}}. `{{.FullPath}}` — {{.KindLabel}} for {{.ScopeLabel}}
{{end}}{{range .Docs}}
---

## `{{.FullPath}}`

{{if ne .Kind "local_run"}}If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.
{{end}}{{partial "repodocs_doc_instructions" .}}{{end}}
---

Also make sure the agent instructions file at the repository root (CLAUDE.md, AGENTS.md or the equivalent the agents on this repo read) has a short docs index, and that it links to every file above so agents find them; create a minimal one if it is missing.

