---
key: tool.read_file
version: "1"
params:
    limit: How many lines to return (default 800). Do not lower it to page through a file in small windows — read it in one call.
    offset: First line to return, 1-based (default 1). Only needed for a file too long to return in one call.
    path: Path of the file to read, relative to the workspace root
---
Read a workspace file with line numbers. Returns up to 800 lines per call and tells you the file's total line count, so one call is normally the whole file. Prefer this over reading files through the shell — `cat`, `sed -n`, `head` and `tail` cost an entire agent turn per window and lose the line numbers you need to edit by.
