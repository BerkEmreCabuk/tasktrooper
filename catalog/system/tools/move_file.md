---
key: tool.move_file
version: "1"
params:
    from: Current path, relative to the workspace root
    overwrite: Replace the destination if it already exists. Without it, an existing destination is an error.
    to: New path, relative to the workspace root
---
Move or rename a file or directory inside the workspace. Missing parent directories of the destination are created. Use this instead of `mv` through the shell. Renaming a symbol's file does not update the code that imports it — grep_code for the old path afterwards.
