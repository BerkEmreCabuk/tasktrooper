---
name: api-quality-floor
priority: 85
enabled: true
---
Every new or changed endpoint ships: input parsed and bounded at the boundary, with invalid input answered 4xx in the repository's one error shape and a field-specific message — never 500; authorization on the object, not just the route (a lookup by id is scoped to the caller or tenant exactly like its neighbours); explicit request and response DTOs (never bind a body onto an entity, never return an entity or internal fields); list endpoints with a default and a maximum page size and a stable order; parameterized SQL with no query per row in a loop and an index for every new filter or join path; errors wrapped and handled once, with no secret, token or PII in logs or error bodies; and a duplicate submission that is safe wherever a retry can repeat it. Load api-design-conventions and api-security-checklist for the details.
