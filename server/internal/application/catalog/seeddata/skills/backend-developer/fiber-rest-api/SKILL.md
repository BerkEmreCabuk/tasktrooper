---
name: fiber-rest-api
category: api
description: Use when adding or changing a Go HTTP endpoint with Fiber - thin handlers, centralized domain-error-to-status mapping, boundary validation, and neighbor-consistent middleware
tech_stack: Go
---

# Fiber REST API Patterns

## Overview

A Fiber handler is a translator: HTTP in → service call → HTTP out. Business logic never lives there. The recurring defects are fat handlers, per-handler error mapping that drifts, and endpoints that quietly drop the auth middleware their neighbors have.

**Core principle:** Handlers parse, validate, delegate, and map. Nothing else.

## Rules

- `github.com/gofiber/fiber/v2` with the Goccy JSON encoder.
- **Thin handlers:** parse/validate input → call the application service → map result to response DTO. No business logic, no SQL, no cross-entity rules in the handler.
- **Explicit DTO structs** aligned with domain types — never `map[string]any`. Annotate every handler for Swagger.
- **Validate and bound all input at the boundary:** lengths, ranges, enum values, UUID parsing. Reject early with 400 and a field-specific message.
- **Map domain errors to status in ONE place** (a shared error handler / mapper), not per handler, so the mapping can't drift.
- **One error JSON shape** across the API; message actionable for the caller, internal detail to logs.
- **New endpoints get the SAME middleware chain** (auth, logging, request-id) as their neighbors — copy the adjacent route's wiring, don't invent one.

## Status codes

| Code | When |
|------|------|
| 200 / 201 | success / created |
| 400 | validation / malformed input |
| 401 / 403 | unauthenticated / forbidden |
| 404 | resource missing |
| 409 | conflict (duplicate, invalid transition) |
| 500 | genuinely unexpected only — never for a validation or not-found |

## Worked Example

```go
// thin handler: parse -> validate -> delegate -> map
func (h *TaskHandler) Create(c *fiber.Ctx) error {
    var req createTaskRequest
    if err := c.BodyParser(&req); err != nil {
        return fiber.NewError(fiber.StatusBadRequest, "invalid body")
    }
    if err := req.validate(); err != nil {          // boundary validation
        return fiber.NewError(fiber.StatusBadRequest, err.Error())
    }
    task, err := h.svc.Create(c.Context(), req.Title)  // service owns the rule
    if err != nil {
        return err                                  // central mapper turns domain errs into codes
    }
    return c.Status(fiber.StatusCreated).JSON(taskResponseFrom(task))
}
```

The `<=200` title rule lives in the service/domain, not the handler. The central error middleware maps `domain.ErrInvalidTitle → 400`, `domain.ErrConflict → 409` — every handler benefits.

## Common Mistakes

- Business logic or SQL in the handler.
- `map[string]any` request/response instead of DTOs.
- Each handler mapping its own errors to codes → drift.
- A new route missing the auth middleware its neighbors have.
- 500 for a validation failure.

## Red Flags

- A handler longer than ~30 lines.
- `pgx`/repository calls inside a handler.
- A route registered without the neighbor's middleware chain.
