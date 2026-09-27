---
key: tool.edit_file
version: "1"
params:
    new_string: Replacement text. Pass an empty string to delete the matched text.
    old_string: Exact text to replace, copied from read_file output without the line-number prefix. Must match a single place in the file unless replace_all is set — include surrounding lines to make it unique.
    path: Path of the file to edit, relative to the workspace root
    replace_all: Replace every occurrence in the file rather than requiring a unique match. This is how you remove or rename a symbol across a file in one call instead of one edit per occurrence.
---
Replace an exact string in a workspace file. This is how you change code — not `sed -i` through the shell. The result says how many occurrences changed and on which lines, so you never need to grep afterwards to check whether the edit landed. Use replace_all to change every occurrence in the file in one call.
