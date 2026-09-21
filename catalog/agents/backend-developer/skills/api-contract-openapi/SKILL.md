---
name: api-contract-openapi
category: api
description: Use when adding or changing a Go API endpoint's request/response shape - keep the OpenAPI/Swagger contract accurate, evolve additively, and update every consumer on a breaking change
---
# OpenAPI & API Contracts

## Overview

The API contract is a promise the web and mobile clients depend on. A silent shape change (renamed field, changed type, removed endpoint) compiles fine on the backend and breaks every consumer at runtime.

**Core principle:** The contract is the annotation + DTO in code. Change it deliberately, evolve it additively, and update consumers in the same task when you can't.

## Rules

- Maintain Swagger at `/docs`; every admin and v1 endpoint is documented with exact field names, types, and required-ness.
- **DTOs align with domain types** — no undocumented fields leaking through, no `map[string]any`.
- **A shape change is a breaking change.** Renaming/removing a field or endpoint, or changing a type, breaks consumers. Grep the consumers (`web/src/api.ts`, mobile clients) and update them in the same task, or coordinate explicitly via the task description/interface block.
- **Prefer additive evolution:** add new optional fields rather than rename; deprecate before removing.
- Regenerate/verify docs after handler changes — the docs must match the code.

## Additive vs breaking

| Change | Type | Action |
|--------|------|--------|
| Add optional field | Additive | Safe; document it |
| Add required request field | Breaking | Update all callers same task |
| Rename field | Breaking | Prefer add-new + deprecate-old |
| Change field type | Breaking | New field or coordinated cutover |
| Remove endpoint/field | Breaking | Deprecate first, remove later |

## Worked Example

Task: "rename task `label` to `title` in the API."

Wrong: rename the DTO field and ship — every client reading `label` breaks.

Right (additive cutover):
1. Add `title` alongside `label` in the response; populate both. Document `label` as deprecated.
2. Grep `web/src/api.ts` + mobile client → update them to read `title`.
3. In a later task, once no consumer reads `label`, remove it with a contract note.

Each step keeps every consumer green.

## Common Mistakes

- Renaming/removing a field without grepping consumers.
- Undocumented fields drifting into responses.
- Docs left stale after a handler change.
- A required new request field added with no client update.

## Red Flags

- A DTO field renamed in the diff with no matching `web/src/api.ts` change.
- `/docs` no longer matches the actual response.
- A consumer reading a field the backend just removed.
