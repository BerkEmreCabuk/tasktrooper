---
name: migration-safety
priority: 70
enabled: true
---
Database schema changes require a new migration (up/down pair for Go; Flyway/Liquibase for Java; dotnet ef migrations add for EF Core); never modify existing migrations and never rely on hibernate auto-DDL or EnsureCreated in non-test environments.
