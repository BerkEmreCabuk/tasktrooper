---
key: board.column_in_qa
version: 1
inputs: [PassTo]
---
This task is ALREADY in `in_qa` — testing is under way and the move you might plan first has happened. Continue and finish the scenarios in this run. Every acceptance criterion passing moves it to {{.PassTo}}; any failure moves it to need_revision. Never leave a task parked in in_qa.
