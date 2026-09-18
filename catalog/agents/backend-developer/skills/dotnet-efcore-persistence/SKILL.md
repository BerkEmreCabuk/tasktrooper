---
name: dotnet-efcore-persistence
category: database
description: Use when a .NET task touches the database - EF Core DbContext usage, migrations, query performance, transactions, and when to drop to Dapper or raw SQL
tech_stack: .NET
---

# EF Core Persistence

## Overview

**Core principle:** Schema changes are migrations, migrations are append-only, and every query is written knowing what SQL it produces.

## Migrations

1. Change the entity / `IEntityTypeConfiguration<T>`.
2. `dotnet ef migrations add <PascalCaseName> --project <Infrastructure or Api project> --startup-project <Api project>` — use the same projects the existing migrations were generated with (look at the `Migrations/` folder's location).
3. Read the generated `Up`/`Down`. A column rename generated as drop+add loses data — fix it to `RenameColumn`.
4. Never edit or delete a migration that exists on the default branch. A wrong migration is fixed by a new one.
5. Commit the migration, its `.Designer.cs` and the updated `ModelSnapshot` together.

If `dotnet ef` is missing: `dotnet tool restore` when the repo has `.config/dotnet-tools.json`, otherwise report it — do not install global tools on your own.

## Querying

| Situation | Do |
|-----------|----|
| Read-only query | `AsNoTracking()` and project to a DTO with `Select` |
| Related data | `Include` what you use, or better a `Select` projection; watch for N+1 in loops |
| Large result | Page with `Skip/Take` on a stable `OrderBy`, or keyset pagination |
| Existence | `AnyAsync`, not `CountAsync() > 0` |
| Bulk change | `ExecuteUpdateAsync` / `ExecuteDeleteAsync` instead of load-modify-save loops |
| Hot read path or reporting SQL | Dapper or `Database.SqlQuery<T>` — when the repo already uses them or the LINQ is unreadable |

Always pass the `CancellationToken` to `ToListAsync(ct)`, `SaveChangesAsync(ct)` etc.

## Consistency

- One `SaveChangesAsync` per unit of work is already a transaction. Open an explicit transaction only when several `SaveChanges` calls or raw SQL must commit together.
- Concurrency-sensitive rows get a concurrency token (`[Timestamp]`/`xmin` on PostgreSQL) and the `DbUpdateConcurrencyException` is handled, not swallowed.
- Never build SQL with string interpolation into `FromSqlRaw`; use `FromSql($"...")` (parameterised) or Dapper parameters.

## Testing persistence

Test real SQL against a Testcontainers database (see dotnet-testing-xunit). EF InMemory and SQLite-in-memory behave differently from PostgreSQL/SQL Server for constraints, collation and transactions.

## Red flags

- `ToList()` followed by `Where(...)` — the filter ran in memory.
- A migration named `Fix`, `Update2`, or one that drops a column still read by the running version (deploy the code change first, drop the column in a later release).
- `EnsureCreated()` anywhere outside a test.
