---
key: tool.expand_symbol_context
version: "1"
params:
    depth: Call-graph expansion depth
    file_path: File containing the symbol (disambiguates homonymous symbols)
    max_chunks: Maximum number of chunks to return
    symbol_name: Symbol to expand from
---
Expand symbol context using the dependency graph and retrieve related code chunks.
