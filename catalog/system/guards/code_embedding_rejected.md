---
key: guard.code_embedding_rejected
version: 1
inputs: [StatusCode, Name]
---
semantic search is unavailable on this server (embedding provider rejected the request: HTTP {{.StatusCode}}). Use grep_code for exact symbols/strings, get_repo_tree to browse and read_file to read — do not call {{.Name}} again in this run.
