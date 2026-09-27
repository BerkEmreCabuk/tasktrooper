---
key: briefs.deploywatch.manual_step_schema
version: 1
inputs: []
---
This task changed the DATABASE SCHEMA. Reverting the code does NOT reverse the migration — the schema is still whatever the release left it as. Say so explicitly on the task and name who has to reverse it; do not report the rollback as complete.
