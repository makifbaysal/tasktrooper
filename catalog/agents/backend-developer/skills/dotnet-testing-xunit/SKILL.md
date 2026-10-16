---
name: dotnet-testing-xunit
category: testing
description: Use when writing tests for C#/.NET code - xUnit structure, NSubstitute or the repository's mocking library at ports, WebApplicationFactory for HTTP, Testcontainers Postgres for EF Core, FakeTimeProvider, test-first
tech_stack: .NET
---
# .NET Testing (xUnit)

## Overview

Test-first in C#, same discipline as everywhere (see tdd-workflow): write the failing test, watch it fail, minimal code to pass. This skill is the .NET mechanics for that cycle.

**Core principle:** unit-test the domain and services as plain C# without starting the host. Start the host (`WebApplicationFactory`) only where wiring, serialization, validation or SQL is what you are proving.

## Test levels

| Level | What it covers | Tools |
|-------|-----------------|-------|
| Unit | Domain entity / value object, no mocks | xUnit `[Fact]` / `[Theory]` |
| Unit with fakes | Application service, its ports faked | xUnit + NSubstitute (or the repository's Moq / FakeItEasy) |
| Persistence | Repository / `DbContext` queries against real Postgres | Testcontainers `PostgreSqlContainer` + the repository's migrations |
| HTTP | Endpoint wiring, binding, validation, ProblemDetails, auth | `WebApplicationFactory<Program>` (`Microsoft.AspNetCore.Mvc.Testing`) + `HttpClient` |

Most tests are the first two. Match the repository's framework: xUnit (v3 for new projects; v2 where the repository is on it), NUnit or MSTest — never add a second one.

## Unit test with a faked port

```csharp
public sealed class TaskServiceTests
{
    private readonly ITaskRepository _repo = Substitute.For<ITaskRepository>();
    private readonly FakeTimeProvider _clock = new(DateTimeOffset.Parse("2026-01-01T00:00:00Z"));
    private readonly TaskService _service;

    public TaskServiceTests() => _service = new TaskService(_repo, _clock);

    [Fact]
    public async Task Create_persists_and_returns_the_task()
    {
        var task = await _service.CreateAsync("Write the report", CancellationToken.None);

        Assert.Equal("Write the report", task.Title);
        await _repo.Received(1).AddAsync(task, Arg.Any<CancellationToken>());
    }

    [Theory]
    [InlineData("")]
    [InlineData("   ")]
    public async Task Create_rejects_a_blank_title(string title)
    {
        await Assert.ThrowsAsync<InvalidTaskTitleException>(() => _service.CreateAsync(title, CancellationToken.None));
        await _repo.DidNotReceiveWithAnyArgs().AddAsync(default!, default);
    }
}
```

## Rules

- **Fake ports, never the class under test.** Assert on the returned value or the observable state first; `Received(...)` alone proves only that the fake was called.
- One behaviour per test, named for it (`Create_rejects_a_blank_title`). `[Theory]` + `[InlineData]` / `[MemberData]` for boundary tables (200 characters accepted, 201 rejected).
- **Time is injected:** `TimeProvider` in production code, `FakeTimeProvider` (`Microsoft.Extensions.TimeProvider.Testing`) in tests — never `DateTime.Now` in either.
- **Real Postgres, not the InMemory provider or SQLite**, for anything that runs SQL: the InMemory provider ignores constraints, transactions and SQL translation. One container per test class or collection (`IAsyncLifetime` / a collection fixture), migrations applied once, data reset between tests (Respawn, or a transaction rolled back per test).
- **HTTP tests** go through `WebApplicationFactory<Program>` with `ConfigureTestServices` replacing only the external dependencies (payment gateway, clock), not the services under test. Assert the status code AND the body shape (ProblemDetails `type`/`errors`). On .NET 9 or earlier a top-level-statement `Program` needs `public partial class Program;` for the factory to see it.
- **Assertions:** the repository's library. Do not add FluentAssertions to a project that lacks it — version 8 is commercially licensed; plain `Assert`, Shouldly or AwesomeAssertions are the free options. Do not add Moq to a project that uses NSubstitute or the other way round.
- `async` tests return `Task`, never `async void`; pass `TestContext.Current.CancellationToken` (xUnit v3) where the API takes one.
- Focused run while iterating: `dotnet test --filter "FullyQualifiedName~TaskServiceTests"`; the whole `dotnet test` before you stop. Coverage through the repository's collector (coverlet `--collect:"XPlat Code Coverage"` or Microsoft.Testing.Platform `--coverage`); mutation testing, where the repository runs it, is Stryker.NET.

## Common Mistakes

- Testing EF queries against the InMemory provider and shipping SQL that Postgres rejects.
- A `WebApplicationFactory` test for every service rule — slow, and it hides design problems a plain unit test would show.
- `Thread.Sleep` / `Task.Delay` to wait for time to pass instead of advancing `FakeTimeProvider`.
- Shared static state between test classes run in parallel.

## Red Flags

- The test passes before the production code exists (it tests the fake).
- A unit test that needs Docker.
- `[Fact(Skip = ...)]` added to make the suite green.
