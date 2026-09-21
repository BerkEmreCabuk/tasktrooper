---
name: migration-safety
priority: 70
enabled: true
---
Database schema changes require a new migration (up/down pair for Go; Flyway/Liquibase for Java); never modify existing migrations and never rely on hibernate auto-DDL in non-test environments.
