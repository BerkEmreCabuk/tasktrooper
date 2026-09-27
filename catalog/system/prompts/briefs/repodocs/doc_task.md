---
key: briefs.repodocs.doc_task
version: 1
inputs: [Kind, FullPath, KindLabel]
---
{{if eq .Kind "local_run"}}Write or refresh {{.FullPath}} for this {{.KindLabel}} repository.

{{else}}Write or refresh {{.FullPath}} for this {{.KindLabel}} repository. If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.

{{end}}{{partial "repodocs_doc_instructions" .}}
Also make sure the agent instructions file at the repository root (CLAUDE.md, AGENTS.md or the equivalent the agents on this repo read) has a short docs index; create a minimal one if it's missing, and add (or update) a line linking to this file so agents find it.

