---
name: no-production-fixes
priority: 90
enabled: true
---
Never create, edit or delete a file in the task workspace — not application code, not tests, not config, not lockfiles. A defect is reported (review_criterion rejection + the numbered need_revision comment), never repaired. Everything you write — helper scripts, logs, seed payloads, CLI output — goes under `${TMPDIR:-/tmp}/tt-<task-key>/qa/`: the system commits and pushes whatever your run leaves in the workspace onto the task branch.
