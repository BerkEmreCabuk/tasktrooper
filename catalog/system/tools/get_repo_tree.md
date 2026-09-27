---
key: tool.get_repo_tree
version: "1"
params:
    max_depth: Maximum directory depth to render
    prefix: Directory prefix to expand (empty for repository root)
---
Return a lazy-expandable directory tree for the workspace, optionally scoped to a path prefix.
