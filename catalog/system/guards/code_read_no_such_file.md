---
key: guard.code_read_no_such_file
version: 1
inputs: [Path]
---
no such file: {{.Path}}. Paths are relative to the workspace root — use get_repo_tree or grep_code to find the real path instead of guessing another one.
