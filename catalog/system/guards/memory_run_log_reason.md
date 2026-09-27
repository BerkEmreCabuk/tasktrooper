---
key: guard.memory_run_log_reason
version: 1
inputs: [Code]
---
{{if eq .Code "pinned_pr"}}it is pinned to one pull request{{else if eq .Code "commit_sha"}}it quotes a commit SHA{{else if eq .Code "column_move"}}it records one card's column move{{else if eq .Code "current_run"}}it is about the run you are in, not about anything a later run can reuse{{else if eq .Code "task_branch"}}it names one task's branch{{else if eq .Code "task_key"}}it names a specific board task{{end}}
