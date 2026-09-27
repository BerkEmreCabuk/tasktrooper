---
key: guard.code_embedding_unreachable
version: 1
inputs: [Cause, Name]
---
semantic search could not run just now: embedding provider could not be reached ({{.Cause}}). Retry {{.Name}} once after a moment, otherwise {{partial "code_embedding_fallback_tools" .}}.
