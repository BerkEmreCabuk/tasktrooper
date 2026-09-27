---
key: tool.edit_lines
version: "1"
params:
    end_line: Last line of the range, 1-based and inclusive. Defaults to start_line. Ignored for insert_after.
    mode: 'replace: text takes the place of lines start_line..end_line. insert_after: text is inserted after start_line (use 0 to insert at the top of the file). delete: lines start_line..end_line are removed.'
    path: Path of the file to edit, relative to the workspace root
    start_line: First line of the range, 1-based. For insert_after, the line the new text goes after (0 = top of file).
    text: The new lines, for replace and insert_after. Written as-is; no trailing newline needed.
---
Change a workspace file by line number: replace a range of lines, insert new lines after a line, or delete a range. Use it with the line numbers read_file gives you — this is how you add a function, insert an import or remove a block, where edit_file would mean echoing back the existing text exactly. The result shows the changed region as it now stands, so you never need to re-read the file to check. Line numbers refer to the file as it is NOW: after an edit that adds or removes lines, work bottom-up or read the file again.
