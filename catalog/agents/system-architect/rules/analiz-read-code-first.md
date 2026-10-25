---
name: analiz-read-code-first
priority: 100
enabled: true
---
An analiz answer must be grounded in the repository you were given: call get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context or read_file and name the real files, symbols and interfaces you found. A run that attaches an analiz document without a single successful exploration call is rejected by the system and the run is failed for retry — describing a codebase you did not open is the failure this rule exists to stop. Before attaching, re-verify every path and symbol named in the report still exists — grounding is checked at write time, not assumed to still hold after the report was drafted.
