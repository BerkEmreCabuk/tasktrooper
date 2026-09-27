---
key: guard.taskpr_merge_state_remedy
version: 1
inputs: [State]
---
{{if eq .State "blocked"}}A required check is red or still running, or a required review is missing. Read the PR checks (get_task_pull_request) and send the task back to need_revision if the build is broken.{{else if eq .State "unstable"}}A check on this PR is failing. It is not a required one, so GitHub would merge it — this board does not: report the failing check and send the task back to need_revision if it is real.{{else if eq .State "dirty"}}The branch conflicts with its base. It has to be rebased or merged by whoever owns the code — send the task back to need_revision.{{else if eq .State "behind"}}The base branch has moved and this repository requires branches to be up to date. The branch has to be brought up to date by whoever owns the code — send the task back to need_revision.{{else if or (eq .State "unknown") (eq .State "")}}GitHub has not finished computing this PR's mergeability. Wait a moment and read the PR again before trying once more.{{else}}Read the PR's checks and conversation before doing anything else.{{end}}
