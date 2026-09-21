---
name: hexagonal-boundaries
priority: 80
enabled: true
---
Keep the domain framework-free: no adapter/framework imports (Fiber, pgx, JAX-RS, Spring web, JPA types) in domain or service layers. Cross-layer calls go through ports/interfaces.
