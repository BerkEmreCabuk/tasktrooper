---
key: tool.delete_file
version: "1"
params:
    path: Path to delete, relative to the workspace root
    recursive: Required to delete a directory that is not empty. Deletes everything inside it.
---
Delete a file, or an empty directory, inside the workspace. Pass recursive:true to remove a directory and everything in it. Use this instead of `rm` through the shell. This removes the ENTIRE file. To remove a function, a block or a range of lines from a file that must survive, use edit_lines with mode:delete — there is no delete_lines tool.
