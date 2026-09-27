---
key: guard.planner_reused_prior_id
version: "1"
inputs: [TaskID]
---
task {{.TaskID}} reuses an id from the prior plan; repair tasks need new unique ids and may only reference the prior ones in depends_on
