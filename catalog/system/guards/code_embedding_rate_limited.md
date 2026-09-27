---
key: guard.code_embedding_rate_limited
version: 1
inputs: [StatusCode, RetryAfterClause]
---
semantic search could not run just now: embedding provider is rate-limited (HTTP {{.StatusCode}}{{.RetryAfterClause}}). Wait, then at most one retry — otherwise {{partial "code_embedding_fallback_tools" .}}.
