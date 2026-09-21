---
name: spring-boot-fallback
category: architecture
description: Use when Java is chosen but Quarkus does not fit - build the service with Spring Boot using the same clean layering, only when a required library lacks a Quarkus extension or the repo already standardizes on Spring
tech_stack: Java
---
# Spring Boot Fallback

## Overview

Quarkus is the default (see quarkus-service-architecture). Spring Boot is the fallback — reach for it only when Quarkus genuinely does not fit. The architecture is the same clean layering; only the annotations and container differ.

**Core principle:** Same layered design as Quarkus. Spring is a container choice, not a license to sprawl.

## When Spring Boot instead of Quarkus

- The repository already uses Spring Boot — follow it.
- A required library/integration has no Quarkus extension and no clean CDI alternative.
- The deployment target or team standard mandates Spring.

If none of these hold, use Quarkus.

## Layer mapping (same responsibilities, Spring surface)

| Layer | Quarkus | Spring Boot |
|-------|---------|-------------|
| Resource/Controller | JAX-RS `@Path` | `@RestController` `@RequestMapping` |
| Service | `@ApplicationScoped` | `@Service` |
| Repository | Panache | `JpaRepository` (Spring Data) |
| Validation | `@Valid` + Bean Validation | `@Valid` + Bean Validation |
| Config | `@ConfigProperty` | `@ConfigurationProperties` / `@Value` |
| Transaction | `@Transactional` (service) | `@Transactional` (service) |

## Patterns (identical discipline)

- **Constructor injection only** — Spring injects a single constructor automatically; no `@Autowired` on fields.
- **DTOs (records) at the controller edge**; never return JPA entities from a controller.
- **Bean Validation** on request DTOs, `@Valid` on the controller method.
- Domain invariants live in the domain class, not only in the DTO annotations.
- Global error handling via `@RestControllerAdvice` mapping domain exceptions to status codes — controllers stay thin.

## Worked Example

```java
@RestController
@RequestMapping("/tasks")
class TaskController {
    private final TaskService service;
    TaskController(TaskService service) { this.service = service; }   // constructor injection

    @PostMapping
    ResponseEntity<TaskResponse> create(@Valid @RequestBody CreateTaskRequest req) {
        Task t = service.create(req.title());
        return ResponseEntity.status(201).body(TaskResponse.from(t));
    }
}

@Service
class TaskService {
    private final TaskRepository repo;
    TaskService(TaskRepository repo) { this.repo = repo; }

    @Transactional
    Task create(String title) {
        Task t = Task.create(title);   // domain enforces invariants
        return repo.save(t);
    }
}
```

## Common Mistakes

- Choosing Spring by habit when Quarkus fits — Quarkus is the default.
- `@Autowired` field injection — use the constructor.
- Business logic in `@RestControllerAdvice` or the controller.
- Returning entities instead of DTOs.

## Red Flags

- You chose Spring but can't name why Quarkus didn't fit.
- Fat controller, anemic service.
