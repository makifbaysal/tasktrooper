---
name: node-typescript-service
category: architecture
description: Use when building or extending a TypeScript service on Node.js - Fastify, NestJS, Express or Hono, runtime validation at the boundary, strict types, the repository's ORM and migrations, async error handling, graceful shutdown
tech_stack: TypeScript (Node.js)
---
# TypeScript Service on Node.js

## Overview

A Node.js service is TypeScript at compile time and untyped JavaScript at run time. Every type at the edge — a request body, a header, an environment variable, a queue message, a row from a raw query — is a claim until something checks it.

**Core principle:** the repository's framework, package manager, module system and ORM win. Parse input at the boundary into typed values; keep the domain free of the framework.

## Choosing, only when there is nothing to follow

- **Fastify** for a new HTTP service: schema-first routes, `app.inject()` for tests, pino logging built in.
- **NestJS** when the service is domain-rich and the team wants modules, DI and decorators (the Quarkus/Spring of Node).
- **Express 5** or **Hono** only to match a repository or a platform that already uses them.

## Layers

| Layer | Responsibility |
|-------|----------------|
| Route / controller | Parse and validate input, call one service, map the result and errors to HTTP |
| Service | Use-case orchestration and business rules; takes plain typed values |
| Domain | Types, invariants, pure functions — no framework import |
| Adapter | Database, outbound HTTP, queues — behind an interface the service depends on |

## Patterns

- **Package manager from the lockfile:** `pnpm-lock.yaml` → pnpm, `yarn.lock` → yarn, `package-lock.json` → npm; install with the frozen form (`pnpm install --frozen-lockfile`, `npm ci`). Node version from `.nvmrc`, `.node-version` or `engines`; a new service targets the current Active LTS.
- **Module system from `package.json`:** `"type": "module"` means ESM (relative imports carry `.js` under `NodeNext`); otherwise CommonJS. Never mix them in one package.
- **`strict: true`.** No `any`; `unknown` at the boundary, narrowed by a parser. No non-null `!` to silence a real `undefined`.
- **Validate at runtime at the boundary:** zod (or TypeBox / Fastify JSON schema, class-validator in NestJS — whatever the repository uses) on body, params, query and headers; derive the TypeScript type from the schema (`z.infer`) so the two cannot drift. Environment variables are parsed once at startup by the same means and the process exits on a bad one.
- **Errors in one shape:** a central error handler (`setErrorHandler`, a Nest exception filter, Express 5's error middleware) maps domain errors to 400/404/409 problem+json; an unknown error is logged with its stack and answered 500 without it.
- **No floating promises:** every promise is awaited, returned or explicitly handled (`@typescript-eslint/no-floating-promises` catches the rest). An unhandled rejection crashes the process in current Node.
- **Outbound calls have a deadline:** `fetch(url, { signal: AbortSignal.timeout(5_000) })` or the client's own timeout; retries only for idempotent calls (resilient-io-and-jobs).
- **Never block the event loop:** no `*Sync` file or crypto calls on a request path, no unbounded `JSON.parse` of a large body, CPU-heavy work in a worker thread or a job.
- **Database:** the repository's tool — Prisma, Drizzle, Kysely, TypeORM, MikroORM or `pg` — with parameters, never string-built SQL (`$queryRawUnsafe` and `sql.raw` with input are bugs). Migrations through that tool (`prisma migrate dev --name`, `drizzle-kit generate`), committed with the schema change; postgres-migrations covers safe DDL.
- **Logging:** pino (Fastify's logger, `nestjs-pino`) with structured fields, request id bound per request; no `console.log` in service code; redact secrets and tokens (`redact` option).
- **Graceful shutdown:** on SIGTERM stop accepting connections, finish in-flight requests, close the pool, then exit — Fastify `close()`, Nest `enableShutdownHooks()`.
- **Dependencies:** a new package is a decision (reuse-before-you-write) — exact name from the registry, added with the repository's package manager, lockfile in the same commit.

## Worked Example

`POST /tasks` in Fastify with zod:

```ts
const CreateTask = z.object({ title: z.string().trim().min(1).max(200) });

export function taskRoutes(app: FastifyInstance, service: TaskService) {
  app.post("/tasks", async (req, reply) => {
    const parsed = CreateTask.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).type("application/problem+json").send(problemFrom(parsed.error));
    }
    const task = await service.create(parsed.data.title, req.user.id);
    return reply.code(201).header("location", `/tasks/${task.id}`).send(toResponse(task));
  });
}
```

`TaskService.create` takes a `string` and an owner id and knows nothing of Fastify; `Task.create` enforces the 200-character rule again for callers that are not HTTP.

## Verify

The repository's scripts first (list_component_checks, `package.json`, CI): typically `npm run typecheck` (or `tsc --noEmit`), `npm run lint`, `npm test`, `npm run build`. For the runtime check build once and run the output — `npm run build`, then `PORT=18080 node dist/main.js` in the background with its pid recorded — not a watcher (`npm run dev`, `tsx watch`, `nodemon`), which never exits and restarts under you.

## Common Mistakes

- Trusting `req.body as CreateTaskDto` — a cast is not a check.
- `async` route handlers in Express 4 without a wrapper: the rejection never reaches the error middleware.
- A request path that awaits in a `for` loop where one query or `Promise.all` with a bound would do — or `Promise.all` over an unbounded list.
- `process.env.X!` scattered through the code instead of one parsed config object.

## Red Flags

- `any` in a new signature, or `// @ts-ignore` / `@ts-expect-error` added to get a build green.
- String-concatenated SQL, or a raw query helper fed user input.
- A promise created and never awaited in a request handler.
- `console.log` of a request body that may hold a token or a password.
