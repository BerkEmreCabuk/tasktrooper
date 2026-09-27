---
key: guard.planner_bookkeeping_only
version: "1"
inputs: [TaskID, QuotedTitle, Tools]
---
subtask {{.TaskID}} ({{.QuotedTitle}}) declares nothing but board bookkeeping ({{.Tools}}). Moving a task between columns is not a deliverable: it takes one tool call, nothing verifies it, and the system performs the move to code_review itself when the implementing run finishes. Drop this subtask and name the move in the implementing subtask's description instead
