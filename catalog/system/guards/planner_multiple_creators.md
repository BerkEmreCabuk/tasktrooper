---
key: guard.planner_multiple_creators
version: "1"
inputs: [Count, IDs, Tool]
---
{{.Count}} subtasks can create board tasks ({{.IDs}}), but at most one subtask in a plan may. Give every board task this request needs to ONE subtask — it can open several in a single run — and for the others either drop them or list tool_names without {{.Tool}}, which is what marks a subtask as not creating records (a subtask with no tool_names inherits its agent's whole toolset and counts as a creator)
