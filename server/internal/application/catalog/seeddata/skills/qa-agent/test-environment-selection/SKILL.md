---
name: test-environment-selection
category: qa
description: Decide where to test - boot the task branch locally in the workspace or use the repository's stage deploy target - and never run any test against production
---

# Test Environment Selection

## Overview

Before executing a single scenario, decide WHERE the product will run. There are exactly two allowed places:

1. **Local — the task workspace (default).** Build and boot the backend, worker, and/or frontend from the task branch inside the task workspace. Fastest feedback, fully isolated, always preferred when the repo can run locally.
2. **Stage — the repository's `stage` deploy target.** Used when the project is configured for stage verification, or when the app cannot boot locally (managed secrets, heavy infra the workspace lacks).

**Production is never a test environment.** No scenario is ever executed against the prod base URL or prod data — not a "harmless" GET, not seeding, not cleanup. Automated suites likewise never touch prod, stage, or any shared database (see test-database-seeding).

## How to decide

1. Read the repository settings (`list_repositories`): `build_command`, `test_command`, `verify_command` — and any comment the developer left on the task (a clean run leaves none). These are the project's declared way of running and checking itself.
2. **Local is possible?** Dependencies resolvable, config/example env present, no unavailable external secrets → boot in the workspace following backend-manual-testing / frontend-manual-testing.
3. **Local is not possible or the project verifies on stage?** Call `get_deploy_target(repository_id, env="stage")` and use the target's `base_url` as the address for every request. If no stage target is configured either, you are blocked — see below.

## Stage rules

- **Confirm the change is actually there.** Stage verdicts are meaningless if the task's branch is not deployed: check the repo's pipeline/deploy state (`get_pipeline_status`), a version/build endpoint, or ask via task comment. Never "test" code that is not running in the environment.
- **Stage is shared.** Namespace your test data (`qa-<task-id>-...` prefixes), avoid destructive scenarios (mass deletes, migrations, load tests) — those run locally only — and clean up what you created.
- The stage `base_url` is for requests; the `health_url` is only a liveness probe.

## When neither is possible

If the app cannot boot in the workspace AND no stage target exists (or the change is not deployed there), do not fake a verdict and do not read the code as a substitute. Post a comment stating exactly what is missing (example env file, seed script, run instructions, stage target) and move the task to need_revision: a change that cannot be executed cannot be verified, so it is not done.

## Red Flags

- A connection string or base URL in your commands points at prod → stop immediately.
- "I'll just check this one thing on prod" → no. Local or stage, always.
- Testing on stage without confirming the deploy → verdicts describe the previous version.
