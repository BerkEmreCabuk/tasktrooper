---
key: rag.inject_context
version: 1
inputs: [Chunks]
---
Relevant document excerpts:

{{range .Chunks}}{{.}}
---
{{end}}
