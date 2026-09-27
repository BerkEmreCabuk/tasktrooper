---
key: guard.planner_duplicate_description_turn
version: "1"
inputs: [OtherID, TaskID]
---
tasks {{.OtherID}} and {{.TaskID}} carry the same description under different titles. That is one piece of work planned twice: it executes twice, comments twice and doubles the cost. Merge them, or give each a description of the distinct work it actually does
