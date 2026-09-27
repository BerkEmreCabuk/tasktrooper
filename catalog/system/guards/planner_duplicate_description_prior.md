---
key: guard.planner_duplicate_description_prior
version: "1"
inputs: [TaskID, PriorID]
---
task {{.TaskID}} repeats the description of task {{.PriorID}}, which this run already ran. Renaming a finished instruction does not make it a new one — it runs the same work again and the board shows the round twice. Describe what is still MISSING instead, and reference the finished task in depends_on
