---
key: board.column_in_qa
version: 2
inputs: [PassTo]
---
This task is ALREADY in `in_qa`. Every acceptance criterion passing moves it to {{.PassTo}}; any failure moves it to need_revision.
