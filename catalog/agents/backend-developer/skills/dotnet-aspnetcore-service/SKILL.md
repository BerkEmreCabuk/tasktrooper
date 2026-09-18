---
name: dotnet-aspnetcore-service
category: architecture
description: Use when writing or changing C#/.NET backend code (ASP.NET Core APIs, workers, class libraries) - project layout, DI, options, async and cancellation, error responses, and when NOT to add abstractions
tech_stack: .NET
---

# ASP.NET Core Service Patterns

## Overview

Modern .NET (8+) with nullable reference types on. Follow the repository first: if it uses controllers, add a controller; if it uses minimal APIs, add an endpoint group; if it has MediatR handlers, write a handler. Never introduce a second style into one service.

**Core principle:** The framework already gives you DI, configuration, logging, validation hooks and ProblemDetails. Use them before writing your own layer.

## Before writing code

1. `dotnet build <the .sln>` once, so you know the baseline is green and which warnings already exist.
2. Find the neighbouring feature (the similar endpoint, the similar handler) and copy its shape — folder, naming, registration, tests.
3. Check `Directory.Build.props` / `Directory.Packages.props`: with central package management a `PackageReference` has NO `Version`; the version goes in `Directory.Packages.props`.

## Rules that hold everywhere

| Area | Do | Don't |
|------|----|-------|
| Async | `async Task`/`ValueTask` all the way; accept and pass `CancellationToken ct` through every I/O call | `.Result`, `.Wait()`, `async void` (except event handlers) |
| Nullability | Treat warnings as bugs; `?` only where null is a real state | `!` to silence the compiler |
| DTOs | `record` / `sealed record` for requests, responses and messages | Exposing EF entities from an endpoint |
| Config | `IOptions<T>` bound from a section, validated with `ValidateDataAnnotations().ValidateOnStart()` | Reading `IConfiguration["X"]` deep in a service |
| DI lifetime | Scoped for anything touching a `DbContext`; singleton only for stateless or thread-safe services | Injecting a scoped service into a singleton (captive dependency) |
| Errors | Return `Results.Problem(...)` / `ProblemDetails` with the right status; map domain failures in one place (`IExceptionHandler` or an endpoint filter) | `catch (Exception) { return 500; }` in every endpoint |
| Logging | `ILogger<T>` with message templates: `logger.LogInformation("Order {OrderId} placed", id)` | String interpolation in log calls; logging secrets or tokens |
| Time/IDs | Inject `TimeProvider`; `Guid.CreateVersion7()` when the repo wants sortable ids | `DateTime.Now` in domain logic (untestable) |
| HTTP clients | `IHttpClientFactory` / typed clients, resilience via `AddStandardResilienceHandler()` | `new HttpClient()` per call |

## Abstractions — earn them

- An interface is justified by a second implementation or a test seam at an I/O boundary (external API, clock, queue). A service with one implementation and no I/O does not need `IFooService`.
- Do NOT wrap EF Core in a generic `IRepository<T>`: `DbContext` already is a unit of work and `DbSet<T>` already is a repository. Add a repository only when the repo already has them or a query is complex enough to deserve a name.
- Clean/Onion layering: follow it when the solution already has `Domain`/`Application`/`Infrastructure` projects; don't impose it on a single-project API.

## Minimal API shape (when the repo uses minimal APIs)

```csharp
public static class OrderEndpoints
{
    public static RouteGroupBuilder MapOrders(this IEndpointRouteBuilder app)
    {
        var group = app.MapGroup("/orders").WithTags("Orders");
        group.MapGet("/{id:guid}", GetById);
        return group;
    }

    private static async Task<Results<Ok<OrderResponse>, NotFound>> GetById(
        Guid id, OrderService orders, CancellationToken ct)
    {
        var order = await orders.FindAsync(id, ct);
        return order is null ? TypedResults.NotFound() : TypedResults.Ok(order.ToResponse());
    }
}
```

`TypedResults` keeps the OpenAPI document honest; register the group from `Program.cs` next to the others.

## Verify

`dotnet build` with zero NEW warnings, then `dotnet test` on the affected test projects. If the repo has an `.editorconfig`, run `dotnet format --include <the files you changed>` so your diff matches the house style; do not reformat files you did not touch.

## Red flags

- A new `IXxx` interface with exactly one implementation and no test that substitutes it.
- `Task.Run` inside a request handler to "make it async".
- A `static` mutable field in a service.
- A package added with a `Version` attribute in a repo that has `Directory.Packages.props`.
