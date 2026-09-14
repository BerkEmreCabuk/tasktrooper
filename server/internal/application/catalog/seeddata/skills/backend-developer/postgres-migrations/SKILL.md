---
name: postgres-migrations
category: database
description: Use when changing the database schema in a Go service - ship numbered up/down migration pairs, never edit an applied migration, and keep down migrations truly reversible
tech_stack: PostgreSQL
---

# PostgreSQL Migrations

## Overview

Migrations are append-only history that must replay identically on every environment, forever. The cardinal sin is editing a migration that has already run somewhere — it makes environments diverge silently.

**Core principle:** A wrong migration is fixed by a NEW migration, never by editing the old one.

## Rules

- Schema changes ship as a new numbered pair under `migrations/`: `NNN_name.up.sql` + `NNN_name.down.sql`. Take the next free number.
- **Never edit an applied migration.** History replays; a divergent edit corrupts environments that already ran it.
- **The down migration genuinely reverts the up** — drop what was created, restore what was altered — so rollback is safe and the migration test's up/down cycle passes.
- **Conventions:** UUID primary keys; `created_at`/`updated_at` timestamps; foreign keys `ON DELETE CASCADE` where the child has no life without the parent; an explicit index for every query path the code adds.
- **Idempotent** where it matters: `IF NOT EXISTS`, `ON CONFLICT`, `WHERE` guards on backfills so a re-run does no harm.
- After writing a migration, run the migration test suite — it applies every up and down against a fresh database.

## Worked Example

Adding a nullable `priority` column with an index:

```sql
-- 043_task_priority.up.sql
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'medium';
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks (priority);

-- 043_task_priority.down.sql   (truly reverses the up)
DROP INDEX IF EXISTS idx_tasks_priority;
ALTER TABLE tasks DROP COLUMN IF EXISTS priority;
```

The `DEFAULT` backfills existing rows safely; the down drops both objects so the test's apply-then-rollback cycle returns the schema to exactly where it started. (See migration 041/042 in this repo for the idempotent-DO-block pattern when a change must be conditional.)

## Common Mistakes

- Editing an already-applied migration to "fix" it.
- A down migration that doesn't fully undo the up → rollback test fails.
- A new query path with no supporting index → N+1/seq-scan in review.
- A non-idempotent backfill that double-applies on re-run.
- `hbm2ddl`/auto-DDL thinking — schema comes from migrations only.

## Red Flags

- The diff modifies an existing `migrations/NNN_*.sql` instead of adding a new number.
- The `.down.sql` is empty or a stub.
- A column added with no thought to existing-row defaults.
