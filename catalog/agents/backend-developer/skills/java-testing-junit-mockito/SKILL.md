---
name: java-testing-junit-mockito
category: testing
description: Use when writing tests for Java (Quarkus or Spring) code - JUnit 5 structure, Mockito for collaborators, and framework-native integration tests, test-first
tech_stack: Java
---
# Java Testing (JUnit 5 + Mockito)

## Overview

Test-first in Java, same discipline as everywhere (see tdd-workflow): write the failing test, watch it fail, minimal code to pass. This skill is the Java mechanics for that cycle.

**Core principle:** Unit-test the domain and services as plain Java without booting the framework. Reserve framework-integration tests for the boundary (HTTP, persistence).

## Test layers

| What | Test type | Tools |
|------|-----------|-------|
| Domain entity / value object | Pure unit, no mocks | JUnit 5 |
| Service (mock its ports) | Unit with mocks | JUnit 5 + Mockito |
| Resource/Controller + DB | Integration (boots framework) | `@QuarkusTest` / `@SpringBootTest`, Testcontainers for DB |

Most tests are the first two — fast, no container. Boot the framework only where you're testing wiring, serialization, or SQL.

## JUnit 5 + Mockito unit test

```java
class TaskServiceTest {
    private final TaskRepository repo = mock(TaskRepository.class);
    private final TaskService service = new TaskService(repo);   // constructor injection pays off

    @Test
    void create_persists_and_returns_task() {
        Task t = service.create("Write the report");
        verify(repo).persist(any(Task.class));   // or save(...) for Spring
        assertEquals("Write the report", t.title());
    }

    @Test
    void create_rejects_blank_title() {
        assertThrows(InvalidTaskTitle.class, () -> service.create("  "));
        verifyNoInteractions(repo);              // invalid input never hits the DB
    }
}
```

## Rules

- **Mock collaborators (ports), never the class under test.** A test whose asserts only exercise the mock proves nothing — assert on the real object's behavior/return.
- One behavior per `@Test`, named for the behavior (`create_rejects_blank_title`).
- Prefer real value objects over mocking them — they're cheap to construct.
- `@ParameterizedTest` for boundary tables (200 chars ok, 201 rejected).
- Integration tests use Testcontainers for a real Postgres, not H2 — test against the DB you deploy on (see test-database-seeding).
- AssertJ (`assertThat(...)`) is fine and reads well; match the repo's existing assertion style.

## Common Mistakes

- `@SpringBootTest`/`@QuarkusTest` on everything — slow, and hides design problems that plain unit tests would surface.
- Mocking the service you're testing.
- Asserting `verify(mock)` only, never checking the returned/observable value.
- H2 in tests but Postgres in prod — dialect gaps ship bugs.

## Red Flags

- The test passes without the production code being written (it tests the mock).
- A unit test that needs a running database.
- No failing-first run — you wrote the test after the code.
