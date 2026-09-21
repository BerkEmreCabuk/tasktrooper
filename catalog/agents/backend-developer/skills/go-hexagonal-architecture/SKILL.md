---
name: go-hexagonal-architecture
category: architecture
description: Hexagonal architecture in Go - domain at the center, ports as interfaces, adapters at the edge
tech_stack: Go
---
# Hexagonal Architecture in Go

## Layers

- **Domain** (innermost): entities, value types, domain errors. Imports nothing from the outer layers — no Fiber, no pgx, no HTTP, no JSON tags driving behavior.
- **Ports**: interfaces in internal/port describing what the application needs (repositories, clients) and offers (services).
- **Application**: use-case services orchestrating domain logic through ports. Depends on domain and ports only.
- **Adapters**: implementations at the edge — HTTP handlers, PostgreSQL repositories, external clients. Adapters import inward; nothing imports an adapter.

## Rules

- Dependency injection via constructors: `NewService(store port.Store, ...)`. No globals, no init-time wiring.
- Cross-layer calls go through ports. A service never touches pgx directly; a handler never contains business logic.
- New behavior starts with the port interface and its domain types, then the service, then the adapter — the test for the service uses a mock of the port.
- When adding a dependency, ask: does the domain need to know this exists? If yes, model it as a port; if no, keep it inside the adapter.

## Smell Checklist

- `import "github.com/gofiber/fiber/v2"` anywhere under internal/domain or internal/application → boundary violation.
- A struct with both business rules and SQL strings → split into service + repository adapter.
- A handler longer than ~30 lines → logic belongs in the application service.
