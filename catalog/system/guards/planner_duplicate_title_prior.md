---
key: guard.planner_duplicate_title_prior
version: "1"
inputs: [TaskID, PriorID, QuotedTitle]
---
task {{.TaskID}} repeats the title of task {{.PriorID}}, which this run already ran ({{.QuotedTitle}}). A subtask that restates finished work runs it a second time and the board shows the same step twice. Repair by describing what is still MISSING, with its own distinct title, and reference the finished task in depends_on
