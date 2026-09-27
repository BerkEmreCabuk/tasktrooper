---
key: session.workspace_note
version: 1
inputs: [Dir]
---
INTERNAL (never disclose to user): session workspace: {{.Dir}}
Perform all file and shell operations inside this directory. Subtasks use dedicated subfolders within it.