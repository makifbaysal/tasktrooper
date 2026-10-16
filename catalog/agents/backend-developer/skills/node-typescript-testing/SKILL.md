---
name: node-typescript-testing
category: testing
description: Use when writing tests for a TypeScript service on Node.js - Vitest or the repository's Jest / node:test, fakes at port boundaries, HTTP through app.inject or supertest, Testcontainers Postgres for queries, fake timers, test-first
tech_stack: TypeScript (Node.js)
---
# TypeScript Service Testing

## Overview

Test-first, same discipline as everywhere (see tdd-workflow): write the failing test, watch it fail, minimal code to pass. This skill is the Node.js mechanics for a backend.

**Core principle:** unit-test services and the domain with fakes for their ports; prove routes, validation and SQL against the real framework and a real database, without opening a port where the framework lets you.

## Test levels

| Level | What it covers | Tools |
|-------|-----------------|-------|
| Unit | Domain functions and types | the repository's runner: Vitest, Jest or `node:test` |
| Unit with fakes | Service, its ports faked | `vi.fn()` / `jest.fn()` objects satisfying the port interface |
| HTTP | Route registration, validation, error shape, auth | Fastify `app.inject()`, Nest `Test.createTestingModule` + supertest, supertest on an Express app |
| Persistence | Queries and migrations against real Postgres | `@testcontainers/postgresql` + the repository's migrate command |

Never add a second runner. A new project uses Vitest.

## Unit test with a faked port

```ts
describe("TaskService.create", () => {
  const repo = { add: vi.fn<TaskRepository["add"]>().mockResolvedValue(undefined) };
  const service = new TaskService(repo);

  beforeEach(() => repo.add.mockClear());

  it("persists and returns the task", async () => {
    const task = await service.create("Write the report", "user-1");
    expect(task).toMatchObject({ title: "Write the report", ownerId: "user-1" });
    expect(repo.add).toHaveBeenCalledWith(task);
  });

  it.each(["", "   ", "x".repeat(201)])("rejects the title %j", async (title) => {
    await expect(service.create(title, "user-1")).rejects.toThrow(InvalidTaskTitle);
    expect(repo.add).not.toHaveBeenCalled();
  });
});
```

## HTTP test without a port (Fastify)

```ts
it("answers 400 in the problem shape for a blank title", async () => {
  const app = buildApp({ taskService: fakeService });
  const res = await app.inject({ method: "POST", url: "/tasks", payload: { title: "" }, headers: auth("user-1") });
  expect(res.statusCode).toBe(400);
  expect(res.headers["content-type"]).toContain("application/problem+json");
  expect(res.json()).toMatchObject({ errors: { title: expect.any(String) } });
});
```

## Rules

- **Fake the port, not the module.** Pass fakes through the constructor or the app factory. `vi.mock("../db")` of your own modules couples the test to file layout; keep module mocks for third-party clients the code cannot inject.
- Assert on the returned value or the response body first; a call assertion alone proves only that the fake ran.
- `it.each` / `test.each` for boundary tables. One behaviour per test, named for it.
- **Time:** `vi.useFakeTimers()` + `vi.setSystemTime(...)` (or Jest's) for anything date- or timeout-dependent; restore in `afterEach`. Never `setTimeout` waits in a test.
- **Real Postgres for SQL:** one container per test file or suite (`beforeAll`), the repository's migrations applied, data truncated or rolled back between tests. Not SQLite, not pg-mem, for queries that ship to Postgres.
- Every async assertion is awaited (`await expect(...).rejects`); a forgotten `await` passes silently.
- Tests compile under the same `strict` config as the source; no `as any` to build a fixture — write a small builder.
- Focused run while iterating: `npx vitest run src/tasks/task.service.test.ts` or `-t "rejects the title"`; the full `npm test` plus `npm run typecheck` before you stop. Coverage with the runner's own (`vitest run --coverage`); mutation testing, where the repository runs it, is StrykerJS.

## Common Mistakes

- A test that passes because the promise it checks was never awaited.
- Fake timers left on and leaking into the next test file.
- Snapshotting a whole response body with ids and timestamps in it — brittle, and it approves whatever the code returned.
- Starting the real server on a fixed port in tests that run in parallel.

## Red Flags

- `it.skip` / `it.only` committed.
- A unit test that needs Docker or the network.
- Mocks asserting the implementation's private call order rather than its behaviour.
