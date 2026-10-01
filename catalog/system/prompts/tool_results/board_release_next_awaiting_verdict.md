---
key: tool_results.board_release_next_awaiting_verdict
version: 1
---
Read query_runtime_logs and list_runtime_errors — pass the release's component, and read errors from well before deployed_at, judging a group by its own first_seen rather than by `new` alone — then call finish_release or rollback_release.
