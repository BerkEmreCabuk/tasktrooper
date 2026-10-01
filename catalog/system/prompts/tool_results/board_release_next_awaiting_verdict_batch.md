---
key: tool_results.board_release_next_awaiting_verdict_batch
version: 1
---
Read get_release for the build/publish evidence (workflow run, local_run, or store_builds) and any smoke checks, then call finish_release or rollback_release. Read query_runtime_logs and list_runtime_errors too when the component has a bound runtime environment (pass its component); when it does not, say so explicitly in the finish note instead of treating the gap as a pass.
