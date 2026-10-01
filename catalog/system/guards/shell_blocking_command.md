---
key: guard.shell_blocking_command
version: 1
inputs: [Segment, What]
---
refused: `{{.Segment}}` starts {{.What}}, which runs until it is interrupted. Run in the foreground it cannot finish — it would hold this tool until the timeout and return nothing but a partial log. To check that the code works, run the build, typecheck or test command instead. If you genuinely need the process up, start it detached and read its log from your task's own scratch directory, never the fixed `/tmp/dev.log` (concurrent agents collide there): `mkdir -p /tmp/tt-<task key> && {{.Segment}} > /tmp/tt-<task key>/dev.log 2>&1 &` then `sleep 5; cat /tmp/tt-<task key>/dev.log`.
