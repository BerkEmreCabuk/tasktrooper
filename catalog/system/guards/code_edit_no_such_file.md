---
key: guard.code_edit_no_such_file
version: 1
inputs: [Path]
---
no such file: {{.Path}}. Paths are relative to the workspace root — use get_repo_tree or grep_code to find it, or write_file to create it.
