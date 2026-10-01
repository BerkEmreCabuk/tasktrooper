---
name: migration-safety
priority: 70
enabled: true
---
Schema changes ship as a NEW migration in the repository's own tool and layout (golang-migrate up/down pair, goose `-- +goose Up/Down`, Flyway `V…__name.sql`, Liquibase changeset, or the repo's up-only files). Never edit a migration that has run anywhere, and never rely on Hibernate auto-DDL outside tests. `rollback_release` reverts code and never runs a down migration, so the new schema must keep working with the previous release's code (expand/contract), and the task's `rollback_plan` says what to do with it — including whether the down migration is still safe to run once data has been written against the new schema since deploy (usually it is not: it silently drops that data). Load postgres-migrations.
