---
name: api-contract-testing
category: qa
description: API contract testing with real requests
---
# API Contract Testing

Test endpoints with curl or httpie against the running service.

Verify: HTTP status codes, response body shape (field names, types), error payload format, auth required vs public, and idempotency where claimed.

Save each request/response pair as evidence for the task comment.

Contract source of truth is the task description and existing API docs — not the handler source.
