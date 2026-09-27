---
key: tool.write_file
version: "1"
params:
    content: Full contents of the file. Written verbatim; a trailing newline is added when missing.
    path: Path of the file to write, relative to the workspace root
---
Create a workspace file, or replace one whole. Parent directories are created as needed. This is how you write a new file — not a shell heredoc, which mangles backticks, quotes and template literals. To change part of an existing file use edit_file or edit_lines instead; this overwrites everything.
