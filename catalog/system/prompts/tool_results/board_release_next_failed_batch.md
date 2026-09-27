---
key: tool_results.board_release_next_failed_batch
version: 1
---
Read get_release: for a local run, its local_run.tail (and local_run.log_path); for github_actions, call get_deploy_logs. Then call rollback_release if the bad code is live or sitting on the default branch — or, if nothing can be done from here, report that on the task.
