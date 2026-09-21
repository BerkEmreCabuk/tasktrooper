The merge landed and the release is being watched here. **Find out what production did with the merge commit.**

### 2. Watch the deploy

Call `get_task_deploy_status`. It reports what production did with **that merge commit** — not with the branch, not with "the latest deploy", with the exact commit your merge produced. It answers for both kinds of repository: one whose deploy is a GitHub Actions job, and one that deploys on push (Vercel and similar), where the signal is the commit status the provider writes.

- **`pending`** — the call does not return a status. It parks this task and your run ends. That is correct and expected: **do not poll, do not sleep, do not call it in a loop.** You (or the next run) will be woken with the answer when the deploy settles.
- **`success`** — production is running this task's code, and the board already says so: post nothing. There is a health window after this, and an incident opened inside it belongs to this release; if you are woken again with one, go to step 3.
- **`no_signal`** — nothing deployed this commit. Two very different reasons produce it, and you have to tell them apart before you answer:
  1. **The repository deploys some other way.** Look for its own procedure — a deploy script, a Makefile target, the deploy steps in its README or `.ai` docs. If it has one, follow exactly those steps with `run_terminal` and then verify the environment answers (its health or base URL). This is a deploy, so treat a failure of it exactly like a failed pipeline: report what failed and stop, do not improvise a different way to ship.
  2. **CI could not run at all.** GitHub Actions is out of minutes, over its spending limit, or disabled for the repository — the pipeline comment on the task says so when that is what happened. That is not a code problem and it is not something you can fix: if the repository also has no local deploy path, move the task to `blocked` and say, in one comment, that the change is merged but undeployed and why.

  Never leave a merged, undeployed task sitting in `done` as if it had shipped.
- **`failure`** — go to step 3.

### 3. Roll back

Read the log first: `get_deploy_logs` returns a summary of the failing Actions job (or the environment's own `logs_url` with `source: logs_url`). Post what actually failed, with the relevant lines — not the whole log.

Then call `rollback_task_release` with the trigger (`deploy_failed` or `health_incident`). You do not choose the mechanism: where a deploy workflow exists it re-deploys the last known-good commit, and where the host deploys on push it reverts the merge commit on the default branch. It refuses — without changing anything — when the task never merged, when its commit is not what the environment is currently running (someone else released after you; rolling back would undo THEIR change), or when nothing actually went wrong.

If it returns **`proposed: true`**, `auto_rollback` is off for that environment and nothing was executed. That is the correct outcome: post the proposal, say plainly that a human has to confirm it, and stop. Do not look for another way to roll production back.

**Whatever it returns, it returns `manual_steps`, and those are yours.** A rollback undoes code. It does not reverse a database migration, turn a feature flag back off, purge a cache or un-send anything. The task's own `rollback_plan` / `before_deploy` fields say which of those apply — read them and follow them. Perform every step you can and **say explicitly, on the card, which ones you could not**. A rollback reported as complete when half of it was not is worse than one that admits what it did not do.

Nothing else is yours in these columns. Do not test (that was `in_qa`), do not edit or commit code, and **never move the task to `released`** — releasing is a production deploy dispatched by its own path, and moving the card there yourself would announce a deploy that never happened.