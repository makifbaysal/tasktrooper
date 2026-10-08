---
name: access-control-and-idor
category: security
description: Use when the diff adds or changes an endpoint, resolver, RPC, job or query that takes an object id, a role check, a request binding or a tenant filter - BOLA/IDOR, function-level authorization, mass assignment and tenant scoping
tech_stack: Web & API
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP API Security Top 10 2023, OWASP Top 10:2025 and ASVS 5.0 cited by name only
---
# Access Control and IDOR

## Overview

Broken access control is the most common serious finding in real diffs, and it rarely looks dangerous: there is no `exec`, no string-built query — just a lookup by id with nothing after it. You find it by asking one question of every entry point the diff touches:

**"If user A sends user B's id, what stops it?"**

If the answer is not a line of code you can point at, it is a finding.

## 1. Object-Level Authorization (BOLA / IDOR — CWE-639, CWE-862)

Enumerate every id the new code accepts: path, query, body, header, nested in a JSON object, inside a batch array, in a GraphQL argument, in a WebSocket message. Follow each to the data layer.

```go
// ❌ any authenticated user reads any invoice
inv, err := h.repo.Get(ctx, r.PathValue("id"))

// ✅ scoped to the caller's organisation in the query itself
inv, err := h.repo.GetForOrg(ctx, r.PathValue("id"), auth.OrgID(ctx))
```

```kotlin
// ❌ Spring: findById alone, the principal is never consulted
@GetMapping("/orders/{id}") fun get(@PathVariable id: UUID) = repo.findById(id)

// ✅ ownership is part of the lookup
@GetMapping("/orders/{id}") fun get(@PathVariable id: UUID, @AuthenticationPrincipal u: User) =
    repo.findByIdAndCustomerId(id, u.customerId) ?: throw NotFound()
```

Check the **second id** too: an endpoint that verifies the project in the path, then updates the task whose id is in the body, without checking the task belongs to that project.

```ts
// ❌ project ownership checked, task id from the body never tied to it
await assertProjectOwner(req.params.projectId, user.id);
await db.task.update({ where: { id: req.body.taskId }, data: { title } });

// ✅ the task is looked up inside the checked project
await db.task.update({ where: { id: req.body.taskId, projectId: req.params.projectId }, data: { title } });
```

## 2. Function-Level Authorization (BFLA — CWE-285, CWE-863)

A privileged action needs a role check, not just "logged in".

- Spring Security: a new `requestMatchers(...).permitAll()` or `.authenticated()` on an admin path; a `@PreAuthorize` removed from a method; a check on the URL that a second mapping of the same handler bypasses.
- Quarkus: a new resource method with no `@RolesAllowed` where its class's siblings have one; `@PermitAll` added.
- Express/Nest/Fastify: a route registered before the auth middleware, or on a router that never mounts it.
- Django/DRF: `permission_classes = [AllowAny]`, a view missing `@login_required` beside decorated siblings.

```python
# ❌ DRF: a staff-only action reachable by any authenticated user
@action(detail=True, methods=["post"])
def refund(self, request, pk=None): ...

# ✅ the action carries the same permission as the admin viewset
@action(detail=True, methods=["post"], permission_classes=[IsAdminUser])
def refund(self, request, pk=None): ...
```

## 3. Property-Level Authorization (mass assignment — CWE-915; excessive exposure — CWE-213)

Binding a request straight onto an entity lets the client set fields it should not: `role`, `isAdmin`, `ownerId`, `orgId`, `price`, `emailVerified`.

```js
// ❌ Mongoose: whatever the client sends becomes the document
const user = await User.create(req.body);

// ✅ an explicit allowlist of client-settable fields
const { name, email } = req.body;
const user = await User.create({ name, email });
```

Equivalents: Django `ModelForm` / DRF serializer with `fields = "__all__"`; Spring `@ModelAttribute` or `@RequestBody` on a JPA entity; Rails `params.permit!`; Pydantic/request DTO that includes `role`; Go `json.Decode` into the domain struct. The response side is the mirror: returning the entity serialises `passwordHash`, internal flags or other users' rows in a list.

## 4. Tenant Scoping

In multi-tenant code, every read and write needs the tenant predicate — including the ones people forget:

- list/search/export/count endpoints and their pagination cursors;
- background jobs and queue consumers that receive an id from a message;
- caches keyed by object id without tenant (one tenant warms, another reads);
- object storage keys built from a user-supplied filename;
- GraphQL nested resolvers (the parent was checked, the child field loads by raw id);
- batch endpoints taking `ids: [...]` — each id must be checked, not the first.

## 5. Failing Open (CWE-280, OWASP A10:2025)

```go
// ❌ a lookup error is treated as "no restriction"
perms, err := h.authz.For(ctx, userID)
if err == nil && !perms.CanDelete { return ErrForbidden }
return h.repo.Delete(ctx, id)

// ✅ any error denies
perms, err := h.authz.For(ctx, userID)
if err != nil || !perms.CanDelete { return ErrForbidden }
```

Also: a `switch` on role with a permissive `default`; a feature flag that disables the check when unset; an inverted comparison.

## Severity Guide

- Reading or changing another tenant's or user's data → HIGH; CRITICAL when unauthenticated, when it reaches credentials or payment data, or when it is admin takeover.
- Exposure of non-sensitive fields of another user → MEDIUM.

## Common Mistakes

- Reporting "ids are guessable": UUIDs are not, and guessability is not the bug — the missing check is. A sequential id with a correct check is fine.
- Missing a check that exists one layer down: read the repository method; `GetForOwner` may already scope the query.
- Reporting a missing check in client code — find the server endpoint and judge that.
- Treating 403 vs 404 as a vulnerability. Follow the repo's convention; it is not your finding.

## Red Flags

- `WHERE id = $1` with no second predicate on a caller-scoped resource.
- `findById`, `get_object_or_404(Model, pk=pk)`, `Model.findByPk(id)` returning straight to the response.
- A new route that its neighbours' middleware chain does not cover.
- A request DTO that is also the entity.
- An authorization helper whose error path returns `nil`, `true` or falls through.

## References (names and links only)

[OWASP API Security Top 10 2023](https://owasp.org/API-Security/editions/2023/en/0x11-t10/) — API1 BOLA, API3 BOPLA, API5 BFLA · [OWASP Top 10:2025 A01 Broken Access Control](https://owasp.org/Top10/2025/) · [ASVS 5.0 V8 Authorization](https://github.com/OWASP/ASVS/tree/master/5.0/en)
