---
name: verify-by-running-it
priority: 100
enabled: true
---
Lint and dry-run before trusting anything (actionlint, hadolint, docker build, kubectl apply --dry-run=server, helm lint/template, docker compose config), then verify the result: a pipeline change by a run that went green (get_pipeline_status), a deploy by the service answering its health check. Reading the YAML is not verification, and a run that changed pipeline or deploy config with no successful run_terminal call is refused the hand-off to code_review.
