---
name: java-persistence
category: architecture
description: Use when a Java service reads or writes the database - JPA/Hibernate and Panache/Spring Data mapping, transaction boundaries, and avoiding N+1 and entity-over-the-wire leaks
tech_stack: Java
---

# Java Persistence (JPA / Panache / Spring Data)

## Overview

Persistence is where Java services most often leak — lazy-loading exceptions, N+1 queries, entities serialized over HTTP. Keep persistence at the repository boundary and mapped deliberately.

**Core principle:** Entities are a persistence detail. They never cross the HTTP boundary and they never carry business logic that belongs in the domain.

## Mapping surface

| Concern | Quarkus (Panache) | Spring Data |
|---------|-------------------|-------------|
| Entity | `@Entity` + `PanacheEntityBase` or plain `@Entity` | `@Entity` |
| Repository | `implements PanacheRepository<Task>` | `extends JpaRepository<Task, UUID>` |
| Query | `find("status", s)` / HQL | derived methods / `@Query` |
| Transaction | `@Transactional` on the service | `@Transactional` on the service |

## Rules

- **Transaction boundary = the service method**, never the repository or the resource. One use-case = one transaction.
- **Never return entities from a controller/resource.** Map to a DTO inside the service (or a JPA projection). Returning entities causes lazy-init serialization errors and leaks the schema.
- **Fetch deliberately to avoid N+1.** A loop that touches a lazy collection per row issues a query per row. Use a `JOIN FETCH` / entity graph / `@BatchSize`, and verify with SQL logging in a test.
- **Bound every list query.** Page (`Page`/`Pageable`, Panache `page(...)`) — never `findAll()` on a growing table.
- **Migrations own the schema, not `hibernate.hbm2ddl=update`.** Set validate/none in non-test envs; schema changes go through a migration (Flyway/Liquibase), never auto-DDL in production.
- Optimistic locking (`@Version`) on entities with concurrent updates.

## Worked Example — killing an N+1

```java
// ❌ N+1: one query for projects, then one per project for its tasks
List<Project> projects = repo.listAll();
projects.forEach(p -> total += p.getTasks().size());   // lazy hit per row

// ✅ one query with a fetch join
@Query("select distinct p from Project p left join fetch p.tasks")
List<Project> findAllWithTasks();
```

Add a test that asserts the query count (Hibernate statistics or a Testcontainers SQL log) so the N+1 can't creep back.

## Common Mistakes

- `@Transactional` on the repository or resource instead of the service.
- Serializing entities in the HTTP response.
- `findAll()` with no paging.
- `hbm2ddl=update` in a real environment instead of migrations.
- Testing on H2 where the SQL dialect differs from Postgres.

## Red Flags

- `LazyInitializationException` in an HTTP response → you serialized an entity outside the transaction.
- Query count scales with row count → N+1.
- Schema changes with no migration file in the diff.
