---
key: orchestrator.board_write_not_landed_reason
version: "1"
inputs: [Declared]
---
This subtask's whole deliverable is the board write it declares ({{.Declared}}), and no such call succeeded — either it was never made or the board rejected it. Make the call, read what it returns, and if it is rejected say so with the exact error instead of reporting the work as done.