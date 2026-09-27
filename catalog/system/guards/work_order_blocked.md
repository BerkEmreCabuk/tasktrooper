---
key: guard.work_order_blocked
version: 1
inputs: [Labels]
---
work order: this task is blocked until these are done: {{join ", " .Labels}}
