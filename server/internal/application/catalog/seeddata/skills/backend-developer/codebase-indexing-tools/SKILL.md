---
name: codebase-indexing-tools
category: tools
description: Code search and indexing tools - explore before changing
---

# Code Search and Indexing Tools

- codebase_search: semantic exploration — "where is task dispatch decided", concepts rather than exact strings.
- grep_code: exact matches — symbol names, error strings, config keys.
- get_repo_tree: structure overview before diving in.
- expand_symbol_context: focused read of one function/type with its surroundings instead of whole files.
- read_file: read a file with line numbers, up to 800 lines in one call. This is how you read source — never `cat`, `sed -n '10,20p'`, `head` or `tail` through run_terminal: each of those costs a whole agent turn per window, and paging a 1200-line file ten lines at a time spends the run's entire budget on scrolling.
- edit_file / edit_lines / write_file / delete_file / move_file: how you CHANGE code. `sed -i`, `rm`, `mv` and heredocs through the shell are not the way — they are silent about what they matched, so you burn a second turn grepping to check. Every file tool reports the count, the line numbers and the region as it now reads; that is the confirmation. Removing a symbol from a file is ONE `edit_file` with `replace_all`, not one call per occurrence.

Workflow: explore before changing. Find the existing pattern for what you're about to do (the neighboring endpoint, the similar service) and follow it. Code written without looking at its neighbors is the top source of review findings.
