---
name: performance-smoke-testing
category: qa
description: Performance smoke testing
---
# Performance Smoke Testing

Not full load testing — a smoke check: measure response time of the touched endpoints (time curl ...), repeat 5-10 times, flag anything unexpectedly slow (>1s for simple CRUD) or growing between identical calls (leak suspicion).

For UI: check the built bundle compiles without size warnings and the page renders without console errors.
