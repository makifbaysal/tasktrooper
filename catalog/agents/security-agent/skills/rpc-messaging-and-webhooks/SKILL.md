---
name: rpc-messaging-and-webhooks
category: security
description: Use when the diff adds or changes a gRPC service or interceptor, a GraphQL resolver or subscription, a protobuf/Thrift schema, a queue or pub/sub producer or consumer, an event handler or an inbound webhook - peer identity vs claimed principal, interceptor gaps, per-item and stream authorization, webhook signatures, untrusted producers, enum defaults, replay, duplicate delivery and ordering
tech_stack: Protocols & messaging
source: cloudflare/security-audit-skill (MIT) protocol, RPC and messaging attack classes, adapted to diff review; own wording; OWASP API Security Top 10 2023 and CWE cited by name only
---
# RPC, Messaging and Webhooks

## Overview

Behind the HTTP edge, services talk through RPC calls, queues and callbacks, and two beliefs keep failing there: "it is internal, so it is authenticated" and "it passed the schema, so it is safe". A schema proves a message's shape — not who sent it, which tenant it may touch, whether it was already processed, or whether it is still current.

**Core principle:** at every hop, name the authenticated peer and show how it becomes the principal the handler authorizes. A field inside the message that names a user or tenant is a claim, not an identity.

## 1. gRPC Interceptors and Method Coverage (CWE-306, CWE-862)

```go
// ❌ only unary calls are authenticated; every streaming method is open
srv := grpc.NewServer(grpc.UnaryInterceptor(authUnary))

// ✅ both kinds, and the skip list is exact method names, not prefixes
srv := grpc.NewServer(
	grpc.ChainUnaryInterceptor(authUnary),
	grpc.ChainStreamInterceptor(authStream),
)
```

Also: a skip list matched with `strings.HasPrefix(info.FullMethod, "/pkg.Health")` that also matches `/pkg.HealthAdmin/…`; server reflection registered on a public listener next to unauthenticated methods; grpc-gateway or Connect transcoding that reaches the same handler through an HTTP route the interceptor does not see; identity read from metadata such as `x-user-id` that any client can send.

## 2. Per-Item and Streaming Authorization

A stream, subscription or batch is authorized once, then later items name different resources:

- a bidirectional stream checks the first message's project and trusts the rest;
- a GraphQL subscription authorized at connect keeps delivering after the user's role is revoked;
- a batch RPC with `repeated string ids` checks `ids[0]`;
- a WebSocket that subscribes by channel name builds the channel from client input (`"org." + msg.org`).

Re-check where the scope can change, and bind a subscription to the principal and resources it started with.

## 3. GraphQL Specifics

- Authorization in a field resolver, not only on the root query: the parent was checked, the nested resolver loads by raw id (access-control-and-idor §4).
- A directive or middleware applied to `Query` but not to `Mutation` or `Subscription`.
- Node or `id` lookups (`node(id:)`) that resolve any type by global id without the type's own check.
- Introspection enabled is not a finding by itself; query depth and cost limits are availability (excluded).

## 4. Inbound Webhooks (CWE-345, CWE-347)

```ts
// ❌ JSON re-serialized before verifying; tenant chosen by the body, then its secret used
const evt = JSON.parse(req.body);
const secret = await secrets.forAccount(evt.account_id);
if (sign(JSON.stringify(evt), secret) !== req.header("X-Signature")) return res.sendStatus(401);

// ✅ verify the raw bytes with the secret bound to the endpoint, then parse
const secret = await secrets.forEndpoint(req.params.endpointId);
if (!timingSafeEqualHex(hmac(req.rawBody, secret), req.header("X-Signature"))) return res.sendStatus(401);
const evt = JSON.parse(req.rawBody);
if (evt.account_id !== endpoint.accountId) return res.sendStatus(403);
```

- **Choose the secret before trusting the body.** When the body's account id selects the verification secret, an attacker signs with their own tenant's secret and names the victim's account.
- Verify the raw request bytes; re-serialized JSON breaks honest signatures and tempts a fallback that skips verification.
- Act only after verification; reject a missing signature instead of treating it as "unsigned mode".
- Timestamp tolerance and a stored event id stop replay (authn-sessions-and-tokens §6 for the basic rules).
- For high-value events, re-fetch the object from the provider's API instead of trusting the body's amount or status.

## 5. Queues, Topics and Untrusted Producers

- **The message as control plane:** a consumer that grants roles, deletes data or changes billing because the body says `type: "admin.grant"` — while any lower-trust service, a user-facing API or a public webhook relay can publish to that topic.
- **Namespace built from input:** topic, routing key, subject or consumer group derived from client text; broker wildcards (`#`, `*`, `>`) let one tenant subscribe to all.
- **Envelope vs payload:** authorization uses a header or attribute set by a trusted router while the handler acts on the tenant in the body — or the other way round. Pick the authoritative source and reject a conflict.
- **Dead-letter and error topics:** failing messages with secrets or other tenants' payloads routed to a queue a broader group can read.

## 6. Enum, Default and Version Confusion

```proto
// ❌ proto3 sends 0 when the field is unset — an old or careless client becomes an owner
enum Role { OWNER = 0; EDITOR = 1; VIEWER = 2; }
// ✅ zero is the meaningless, least-privileged value
enum Role { ROLE_UNSPECIFIED = 0; VIEWER = 1; EDITOR = 2; OWNER = 3; }
```

Also: a `switch` on a message type whose `default` branch performs the privileged handling; unknown fields kept and forwarded to a consumer that trusts them; a new field that old consumers ignore while it was meant to restrict them (`readonly: true` added to the producer only).

## 7. Replay, Duplicates and Ordering (CWE-294)

- **At-least-once delivery** plus a side effect that is not idempotent: a credit applied twice, an email sent twice, an invite accepted twice. The dedupe key must be checked atomically with the effect, and scoped by tenant and operation.
- **Stale messages:** a "grant" processed after a newer "revoke" because there is no version or sequence check.
- **Ack before commit** loses a security-relevant event (an audit or revoke); commit before an ack that may fail repeats a mutation.
- **Correlation ids:** a predictable or shared reply id lets one caller's response satisfy another's pending request.

## Severity Guide

An unauthenticated RPC, stream or webhook that changes state → CRITICAL. A forged or cross-tenant event accepted by a privileged consumer → HIGH to CRITICAL. A non-idempotent duplicate with money or privilege impact → HIGH. Replay of a low-impact notification → MEDIUM at most.

## Common Mistakes

- Reporting "internal service, no auth" without showing who can reach it — and reporting it as fine for the same reason. Reachability is the question (source-visibility-needs-validation when it is not in the repository).
- Treating a valid JSON Schema, protobuf or zod parse as an authorization check.
- Reporting at-least-once delivery itself; the finding is the non-idempotent side effect.
- Reporting GraphQL introspection or missing depth limits.

## Red Flags

- `grpc.UnaryInterceptor` with no stream interceptor; `HasPrefix` on `FullMethod`.
- `user_id`, `tenant_id`, `org` or `role` read from RPC metadata or a message body and used for authorization.
- A webhook handler that parses, looks up a secret by a body field, or acts before it verifies.
- A consumer whose side effect has no idempotency key, or checks it after the write.
- A proto enum whose zero value is a real, privileged choice.

## References (names and links only)

[gRPC authentication guide](https://grpc.io/docs/guides/auth/) · [OWASP GraphQL Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/GraphQL_Cheat_Sheet.html) · [OWASP API Security Top 10 2023](https://owasp.org/API-Security/editions/2023/en/0x11-t10/) · CWE-294, 306, 345, 347, 862 · [Cloudflare security-audit-skill](https://github.com/cloudflare/security-audit-skill)
