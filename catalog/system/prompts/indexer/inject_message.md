---
key: indexer.inject_message
version: 1
inputs: [ShowTree, Tree, ShowSkeleton, Skeleton, Chunks]
---
{{if .ShowTree}}## Repository structure
{{.Tree}}
{{end}}{{if .ShowSkeleton}}## Code skeleton
{{.Skeleton}}
{{end}}{{if .Chunks}}## Relevant code
{{range .Chunks}}### {{.Label}}
```{{.Language}}
{{if .ShowSignature}}{{.Signature}}
{{end}}{{.Content}}```

{{end}}{{end}}
