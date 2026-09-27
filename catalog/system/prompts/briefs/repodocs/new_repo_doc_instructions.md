---
key: briefs.repodocs.new_repo_doc_instructions
version: 1
inputs: [Kind, FullPath]
---
{{if eq .Kind "coding_standards"}}Prescribe the conventions for the chosen stack: the formatter and linter to use (add their config files to the repository so they actually run), naming and file-layout conventions, the error-handling style, and the few rules that matter most for this kind of project. Keep it short and concrete — rules, not a tutorial.
{{else if eq .Kind "test_standards"}}Prescribe how this project is tested: the test runner and the exact command to run it (wire it up, with at least one passing example test if there is code to test), which layers to use (unit/integration/e2e — pick what fits this stack), where test files live and how they are named, and how a new feature's tests should be structured.
{{else if eq .Kind "architecture"}}Prescribe the architecture: the layers or modules and the direction of dependencies between them, where each kind of code goes in the directory layout, the data flow for a typical request or job, and the boundaries that must not be crossed.
{{else if eq .Kind "local_run"}}{{partial "repodocs_local_run_script_requirements" .}}Everything in it must match the chosen stack and what this pull request adds — the real package manager, build tool and ports — not a generic template. Do not write a markdown guide instead of, or alongside, the script.
{{end}}