This task came back from review. Read BOTH the task comments and, when the review happened on a pull request, the PR review comments (`list_task_comments` and `get_task_pull_request` re-read them at any time) before you touch the code. No fix without root-cause investigation first: reproduce the failure, trace it to its source, write a failing test that reproduces it, fix at the source, and address EVERY numbered point explicitly. Finish with an updated how-to-test note; the hand-off back to code_review is automatic.

## Standing acceptance criteria

These apply to every code task — they are not written on the card, and the build gate checks all three automatically before this task can be handed on:

1. The project builds. A red build is not a finished task, whatever else is done.
2. The whole test suite passes — including tests you did not write. A test your change broke is your change's problem, not a pre-existing failure to report.
3. New or changed behaviour comes with a unit test that would fail without it, written in this run next to the project's existing tests and in its style. Pure config, copy or asset edits are the exception — say so in your closing comment rather than inventing a test for them.

Ticking the task's own criteria while any of these three is unmet is a false claim: a red result after you stop sends the task back with your name on it.