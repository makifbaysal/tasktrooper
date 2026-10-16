---
name: dotnet-aspnetcore-service
category: architecture
description: Use when building or extending a C# service on ASP.NET Core - Minimal APIs or controllers, DI and options, EF Core with Npgsql, ProblemDetails, HttpClientFactory resilience, async and cancellation
tech_stack: .NET
---
# ASP.NET Core Service (C#)

## Overview

ASP.NET Core is the .NET backend stack: one host for HTTP, DI, configuration, logging and background work. Build in layers so the framework stays at the edges and the domain stays plain C#.

**Core principle:** the repository's shape wins. Minimal APIs or MVC controllers, EF Core or Dapper, Clean Architecture projects or one project — follow what is there. The patterns below are for new code and for the gaps.

## Layers

| Layer | Responsibility | ASP.NET Core surface |
|-------|----------------|----------------------|
| Endpoint | HTTP in/out, binding, status codes | `MapGroup` + `MapPost` (Minimal APIs) or `[ApiController]` controllers |
| Application service | Use-case orchestration, business rules | a class registered in DI, constructor-injected, `CancellationToken` on every async method |
| Domain | Entities, value objects, invariants | plain C#, no ASP.NET or EF attributes driving behaviour |
| Infrastructure | Persistence, outbound HTTP, queues | `DbContext`, typed `HttpClient`, `BackgroundService` |

Dependencies point inward: an endpoint calls a service, a service depends on an interface the infrastructure implements. An endpoint never contains business rules; a service never takes `HttpContext`.

## Patterns

- **Target framework:** keep the repository's `TargetFramework`. A new service targets the current LTS (`net10.0`); never move a repository to a preview SDK in a feature task. `global.json` pins the SDK when the repository has one.
- **Nullable reference types on** (`<Nullable>enable</Nullable>`); treat a new nullable warning as a bug, not noise. Respect `TreatWarningsAsErrors` and `.editorconfig` analyzers.
- **Constructor injection** (primary constructors are fine): `public sealed class TaskService(ITaskRepository repo, TimeProvider clock)`. Lifetimes: `DbContext` scoped, stateless services scoped or singleton, never a scoped dependency captured by a singleton.
- **Options pattern** for configuration: `builder.Services.AddOptions<SmtpOptions>().BindConfiguration("Smtp").ValidateDataAnnotations().ValidateOnStart();` — never read `IConfiguration["..."]` in a service, never hardcode.
- **DTOs at the edge** (records), mapped to and from the domain in the service. Never bind a request onto an EF entity or return one.
- **Validation at the boundary:** DataAnnotations on the request record with `builder.Services.AddValidation()` (Minimal APIs, .NET 10) or `[ApiController]` model validation; FluentValidation where the repository already uses it. The domain enforces its own invariants too.
- **Errors in one shape:** `builder.Services.AddProblemDetails()`, `app.UseExceptionHandler()` and an `IExceptionHandler` that maps domain exceptions to 400/404/409. Endpoints return `TypedResults` (`Results<Created<TaskResponse>, ValidationProblem>`), not ad-hoc anonymous objects. No stack trace in a response body.
- **Async all the way:** `await` every I/O call and pass the request's `CancellationToken` down to EF and `HttpClient`. Never `.Result`, `.Wait()` or `async void` (outside event handlers).
- **EF Core:** `AsNoTracking()` for reads, projection (`Select`) to DTOs instead of loading graphs, `Include` only for what the response needs. Postgres via `Npgsql.EntityFrameworkCore.PostgreSQL`. Raw SQL only through `FromSql`/`SqlQuery` with interpolated parameters (they are parameterized) — never `FromSqlRaw` with string concatenation.
- **Migrations:** `dotnet ef migrations add <Name>`, review the generated `Up`/`Down`, commit the migration and the model snapshot together. Deploys apply an idempotent script (`dotnet ef migrations script --idempotent`) or a migration bundle; `Database.Migrate()` at startup races when several instances start and `EnsureCreated()` never belongs outside tests. postgres-migrations covers safe DDL.
- **Outbound HTTP:** a typed client from `IHttpClientFactory` (`builder.Services.AddHttpClient<PaymentsClient>()`) with `AddStandardResilienceHandler()` (`Microsoft.Extensions.Http.Resilience`) for timeouts, retries and circuit breaking. Never `new HttpClient()` per call.
- **Background work:** a `BackgroundService` that creates a scope per unit of work (`IServiceScopeFactory`) and honours `stoppingToken`.
- **Logging:** `ILogger<T>` with message templates (`logger.LogInformation("Task {TaskId} created", id)`), never string interpolation in a log call; `[LoggerMessage]` source-generated methods on hot paths. No secrets or PII in a log or a ProblemDetails body.
- **Time and ids:** inject `TimeProvider` instead of `DateTime.UtcNow` so tests control the clock; `Guid.CreateVersion7()` for new time-ordered ids where the repository has no id scheme.
- **OpenAPI:** the built-in `builder.Services.AddOpenApi()` / `app.MapOpenApi()` document; keep it in step with api-contract-openapi. New code does not add Swashbuckle.

## Worked Example

`POST /tasks` creating a task:

```csharp
public sealed record CreateTaskRequest([property: Required, StringLength(200)] string Title);
public sealed record TaskResponse(Guid Id, string Title);

public static class TaskEndpoints
{
    public static RouteGroupBuilder MapTasks(this IEndpointRouteBuilder app)
    {
        var group = app.MapGroup("/tasks").RequireAuthorization();
        group.MapPost("/", Create);
        return group;
    }

    private static async Task<Created<TaskResponse>> Create(
        CreateTaskRequest req, TaskService service, CancellationToken ct)
    {
        var task = await service.CreateAsync(req.Title, ct);
        return TypedResults.Created($"/tasks/{task.Id}", new TaskResponse(task.Id, task.Title));
    }
}

public sealed class TaskService(ITaskRepository repo, TimeProvider clock)
{
    public async Task<TodoTask> CreateAsync(string title, CancellationToken ct)
    {
        var task = TodoTask.Create(title, clock.GetUtcNow()); // the domain enforces the 200-character invariant too
        await repo.AddAsync(task, ct);
        return task;
    }
}
```

The endpoint binds and validates; the service orchestrates; `TodoTask.Create` rejects an invalid title even when it is called from a job instead of HTTP.

## Verify

Use the repository's own commands first (list_component_checks, the README, CI). Otherwise: `dotnet build` (warnings read, not skipped), `dotnet format --verify-no-changes` when the repository is formatted with it, and `dotnet test`. For the runtime check build once and run the output — `dotnet publish <Api>.csproj -c Release -o /tmp/tt-<task key>/out`, then `ASPNETCORE_URLS=http://127.0.0.1:18080 dotnet /tmp/tt-<task key>/out/<Api>.dll` in the background — rather than `dotnet run`, whose child process a terminal timeout can orphan.

## Common Mistakes

- Business logic in the endpoint or controller.
- Returning an EF entity (lazy-loading surprises, leaked columns, cycles in the JSON).
- `.Result` / `.Wait()` on a task — thread-pool starvation under load.
- A `CancellationToken` accepted and never passed on.
- `new HttpClient()` in a loop — socket exhaustion; `IHttpClientFactory` exists for this.
- Applying migrations from every pod at startup.

## Red Flags

- `using Microsoft.AspNetCore.*` inside the domain project.
- `FromSqlRaw($"... {input}")` or any SQL assembled from strings.
- A singleton service with a `DbContext` constructor parameter.
- A catch-all `catch (Exception)` that returns 200.
