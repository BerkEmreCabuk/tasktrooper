---
key: guard.board_ungrounded_analysis_grounding
version: 1
inputs: [Tools]
---
An analiz result must be based on the repository, and this run has not read it yet: no {{join ", " .Tools}} call has succeeded. Explore the code first (get_repo_tree for structure, codebase_search for concepts, grep_code for exact symbols, expand_symbol_context to read the parts that matter), then write the analysis naming the real files and interfaces you found.
