---
key: briefs.deploywatch.rollback_dispatched
version: 1
inputs: [Env, WorkflowFile, RollbackOfSHA, Ref]
---
Rolled back {{.Env}} by dispatching {{.WorkflowFile}} at {{.RollbackOfSHA}} (tag {{.Ref}}), returning production to {{.RollbackOfSHA}}.
