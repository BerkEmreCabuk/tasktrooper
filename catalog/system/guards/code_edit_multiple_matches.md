---
key: guard.code_edit_multiple_matches
version: 1
inputs: [Count, Path]
---
old_string matches {{.Count}} places in {{.Path}}. Either pass replace_all:true to change all {{.Count}}, or include the surrounding lines so it matches exactly one.
