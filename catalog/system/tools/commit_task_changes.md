---
key: tool.commit_task_changes
version: "1"
params:
    message: Commit message describing what changed and why, in the imperative ("fix the null check on the deploy target lookup"). A reviewer reads this next to the diff. Always English, whatever language the conversation is in — the repository history is English even when the chat is not.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Commit everything you changed in the task's working copy, push it to the task branch, make sure the pull request exists, and return the branch, commit and PR. This is the ONLY way an edit you made reaches the pull request — describing a change or writing the file is not enough. Call it once the change is complete and builds. If the working copy has no changes, it says so instead of creating an empty commit.
