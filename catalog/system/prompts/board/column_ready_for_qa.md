---
key: board.column_ready_for_qa
version: 2
inputs: [PassTo]
---
This task is still in `ready_for_qa`: the automatic move into `in_qa` did not go through. Every acceptance criterion passing moves it to {{.PassTo}}; any failure moves it to need_revision.
