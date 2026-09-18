---
name: dotnet-testing-xunit
category: testing
description: Use when writing or fixing tests for C#/.NET code - xUnit structure, test doubles, WebApplicationFactory integration tests, Testcontainers, and the coverlet setup the coverage gate needs
tech_stack: .NET
---

# .NET Testing (xUnit)

## Overview

Test-first, same discipline as tdd-workflow: failing test, watch it fail for the right reason, minimal code, refactor. This skill is the .NET mechanics.

**Core principle:** Use the test framework and assertion library the repository already uses. Only a brand-new test project gets to choose, and then it follows the table below.

## Picking the stack (new test projects only)

| Concern | Default | Note |
|---------|---------|------|
| Framework | xUnit v3 (`xunit.v3`) | Keep NUnit/MSTest if the solution already uses them |
| Assertions | Built-in `Assert` or Shouldly | FluentAssertions v8+ is commercially licensed — do not add it to a repo that doesn't already have it |
| Test doubles | NSubstitute | Keep Moq/FakeItEasy if already present |
| HTTP integration | `Microsoft.AspNetCore.Mvc.Testing` (`WebApplicationFactory<Program>`) | Needs `public partial class Program;` in the API if top-level statements are used |
| Real database | Testcontainers (`Testcontainers.PostgreSql` etc.) | Never EF InMemory for behaviour that depends on SQL |
| Coverage | `coverlet.collector` | Without it the board cannot measure coverage for the project |

Create with the repo's existing template if it has one; otherwise `dotnet new xunit -o tests/<Project>.Tests` (ships with the SDK) and move it to `xunit.v3`. Then `dotnet sln add` it and `dotnet add reference` the project under test. `FakeTimeProvider` comes from `Microsoft.Extensions.TimeProvider.Testing`.

## Layers

| What | Test | Speed |
|------|------|-------|
| Domain types, pure services | Plain unit test, no doubles | ms |
| Service with I/O ports | Unit test, substitute the port | ms |
| Endpoint + serialization + DI wiring | `WebApplicationFactory` with the DB swapped for a container | s |
| SQL / migrations | Testcontainers against the real engine | s |

Most tests belong in the first two rows.

## Unit test shape

```csharp
public sealed class OrderServiceTests
{
    private readonly IOrderRepository _orders = Substitute.For<IOrderRepository>();
    private readonly FakeTimeProvider _clock = new(new DateTimeOffset(2026, 1, 1, 0, 0, 0, TimeSpan.Zero));

    [Fact]
    public async Task Place_rejects_an_empty_basket()
    {
        var sut = new OrderService(_orders, _clock);

        var result = await sut.PlaceAsync(new PlaceOrder([]), TestContext.Current.CancellationToken);

        Assert.False(result.IsSuccess);
        await _orders.DidNotReceiveWithAnyArgs().AddAsync(default!, default);
    }

    [Theory]
    [InlineData(0)]
    [InlineData(-1)]
    public void Quantity_must_be_positive(int quantity) =>
        Assert.Throws<ArgumentOutOfRangeException>(() => new OrderLine("sku", quantity));
}
```

Name tests as behaviour (`Place_rejects_an_empty_basket`), one behaviour per test, `[Theory]` for input tables.

## Integration test shape

```csharp
public sealed class OrdersApiTests(ApiFactory factory) : IClassFixture<ApiFactory>
{
    [Fact]
    public async Task Get_unknown_order_is_404()
    {
        var client = factory.CreateClient();
        var response = await client.GetAsync($"/orders/{Guid.NewGuid()}", TestContext.Current.CancellationToken);
        Assert.Equal(HttpStatusCode.NotFound, response.StatusCode);
    }
}
```

`ApiFactory : WebApplicationFactory<Program>, IAsyncLifetime` starts the container in `InitializeAsync` and overrides the connection string in `ConfigureWebHost`.

## Running

- One project: `dotnet test tests/Shop.Api.Tests`
- One test: `dotnet test --filter "FullyQualifiedName~OrderServiceTests.Place_rejects"`
- Read the summary line (`Passed! - Failed: 0, Passed: N`). "Build succeeded" is not a test result.

## Red flags

- `Thread.Sleep` / `Task.Delay` to wait for something — use `FakeTimeProvider` or poll with a timeout.
- A test that only passes when run alone (shared static state, shared DB rows).
- Asserting on a substitute's received calls for something the return value already proves.
- Deleting or `[Skip]`-ing a failing test to get green.
