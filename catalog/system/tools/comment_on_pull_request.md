---
key: tool.comment_on_pull_request
version: "1"
params:
    body: Comment text (markdown). Say what you changed and where, not that you will change it.
    reply_to_comment_id: Id of the review comment to answer, from get_task_pull_request's review_comments. Omit to start a new PR conversation comment.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Post a comment on the task's pull request, or answer one review comment inside its own thread by passing that comment's id. Reply in-thread whenever you are responding to a reviewer — a new top-level comment leaves their thread unanswered. Get the ids from get_task_pull_request.
