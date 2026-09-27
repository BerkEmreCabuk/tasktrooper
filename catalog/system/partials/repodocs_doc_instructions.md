---
key: partial.repodocs_doc_instructions
version: 1
inputs: [Kind, FullPath]
---
{{if eq .Kind "coding_standards"}}Cover: the formatting/lint tooling actually configured, naming and file-layout conventions, error-handling style, and anything this codebase does differently from a generic style guide. Read the existing code before writing — describe what it does, don't prescribe a generic standard.
{{else if eq .Kind "test_standards"}}Cover: the test runner and how to invoke it, the testing pyramid this repo actually follows (unit/integration/e2e — only the layers that exist), coverage expectations if any, and how a new feature's tests should be structured here.
{{else if eq .Kind "architecture"}}Cover: the major components/layers and how they depend on each other, the data flow for a typical request or task, and the boundaries that must not be crossed (e.g. hexagonal layering, module isolation).
{{else if eq .Kind "local_run"}}{{partial "repodocs_local_run_script_requirements" .}}Everything in it must match what this repository actually needs today — its real package manager, build tool and ports — not a generic template. Do not write a markdown guide instead of, or alongside, the script.
{{end}}