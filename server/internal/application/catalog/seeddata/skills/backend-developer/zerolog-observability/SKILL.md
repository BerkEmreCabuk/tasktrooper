---
name: zerolog-observability
category: observability
description: Use when adding logging to Go code - structured zerolog with fields (not string interpolation), correct levels, request correlation, and no secrets or double-logging
tech_stack: Go
---

# Zerolog Structured Logging

## Overview

Logs are for machines first: structured fields you can filter and correlate, not prose. The recurring defects are `fmt.Println` debugging left in, data interpolated into the message instead of fields, the same error logged at every layer, and secrets leaking into logs.

**Core principle:** Log the failure at its site, with fields, once.

## Rules

- `github.com/rs/zerolog` exclusively — never `fmt.Println` / `log.Printf` in production code.
- **Fields, not interpolation:** ids and operation names are structured fields, not baked into the message string.
- **Correlate:** attach `request_id` (and relevant entity ids) so one request is traceable across layers.
- **Don't double-log:** log-and-return the same error at each layer reports it N times. Log at the site that has the most context (usually where it's handled), propagate elsewhere with `%w`.
- **Never log secrets, tokens, or full request bodies** that may carry user data.

## Levels

| Level | Use for |
|-------|---------|
| Debug | development detail, verbose tracing |
| Info | state changes worth auditing (task moved, migration applied) |
| Warn | handled anomaly (retry, fallback taken) |
| Error | a failure needing attention |

## Worked Example

```go
// ❌ prose message, interpolated id, will be logged again by the caller too
log.Error().Msg(fmt.Sprintf("dispatch failed for task %s: %v", id, err))

// ✅ structured fields, error attached, logged once at the handling site
log.Error().
    Err(err).
    Str("task_id", id.String()).
    Str("op", "dispatch").
    Msg("dispatch failed")
// upstream callers propagate with fmt.Errorf("dispatch: %w", err) — they do NOT re-log
```

Now a query like `op="dispatch" level="error"` finds every dispatch failure with its task id, and each failure appears exactly once.

## Common Mistakes

- `fmt.Println`/`log.Printf` left in production paths.
- `Msg(fmt.Sprintf(...))` interpolating ids instead of `.Str(...)` fields.
- The same error logged in the repo, the service, AND the handler.
- Logging a token, password, or full request body.
- Everything at Info (or everything at Error) — levels lose meaning.

## Red Flags

- A message string with `%s`/`+id` inside it.
- The same failure appears three times in the logs for one request.
- A secret or auth header value in a log line.
