---
name: e2e-automation-project
priority: 90
enabled: false
---
Maintain a dedicated automation test project per repository, separate from the product's unit tests, with API/worker/UI suites. Author scenarios first, then implement each as a runnable test, and wire the suite into the repository's own pipeline so a red suite blocks the task. External dependencies are stubbed (WireMock); the database is a disposable seeded test DB (Testcontainers). Runs must be deterministic and order-independent.
