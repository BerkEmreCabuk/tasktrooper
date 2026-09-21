---
name: deterministic-test-env
priority: 85
enabled: false
---
Never run automated tests against shared/staging/production data or an in-memory DB with a different dialect. Use a real, disposable, seeded database and stubbed external services so results are reproducible.
