---
key: tool.search_memory
version: "1"
params:
    owner: all (default) = your memories plus the team's; self = only yours; team = only the team's
    query: Optional semantic search query
    scope: all (default) = this repository's memories plus the global ones; project = only this repository; global = only repository-independent
    top_k: Max results (default 5)
---
Search the memories you can read: your own plus the team's, from this repository plus the global ones. Empty query returns the most recent. Use scope to narrow to project or global memories, and owner to look only at your own or only at the team's.
