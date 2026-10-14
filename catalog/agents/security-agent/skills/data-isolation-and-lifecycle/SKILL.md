---
name: data-isolation-and-lifecycle
category: security
description: Use when the diff stores, caches, indexes, exports, imports, migrates, soft-deletes or revokes access-controlled or multi-tenant data - policy vs query disagreement, bypass clients, tenant-less cache and object keys, signed links, derived copies (search, cache, logs, analytics), export/restore scope, migration defaults, and stale access after delete or revoke
tech_stack: Web & API
source: cloudflare/security-audit-skill (MIT) data-isolation and lifecycle attack classes, adapted to diff review; own wording; OWASP Top 10:2025 A01 and CWE cited by name only
---
# Data Isolation and Lifecycle

## Overview

Endpoint checks protect one door. Data has many copies — a cache entry, a search document, a thumbnail, an export file, a log line, a backup, a queued job — and each copy has its own reader and its own retention. Isolation fails where a copy forgets who owns the data, or where a deleted or revoked record keeps working.

**Core principle:** pick one protected record the diff touches and follow it through every write, read, copy and state change. At each edge, name the code that enforces its owner. A tenant column is not that code.

## 1. A Tenant Field Is Not Isolation (CWE-639, CWE-863)

The owner column exists; the question is which query, policy or key enforces it on this path.

- **Bypass clients:** Supabase's service-role key or a Postgres role with `BYPASSRLS` used in a request handler; a "system" repository that skips the tenant filter, called from a user-facing path.
- **Unscoped escapes:** GORM `Unscoped()` (it also drops the soft-delete filter), Rails `unscoped`, a Django manager that is not the filtered default, Hibernate filters that do not apply to native queries, Prisma middleware that a raw query skips.
- **Policy and query disagree:** row-level security on the table but the new view, function (`SECURITY DEFINER`), materialized view or aggregate reads around it; a join that brings in rows of another tenant through an unscoped relation.

```sql
-- ❌ SECURITY DEFINER runs as the function's owner; a table owner bypasses RLS
--    (unless FORCE ROW LEVEL SECURITY is set), so the caller's policy never applies
CREATE FUNCTION project_stats(p uuid) RETURNS TABLE(...) LANGUAGE sql SECURITY DEFINER
AS $$ SELECT ... FROM tasks WHERE project_id = p $$;
-- ✅ SECURITY INVOKER (the default), or filter on the caller's tenant inside the function
```

## 2. Keys and Namespaces Without the Tenant

Two principals collide on one logical key even though their records have separate owners:

- cache keys built from a user-chosen name (`report:` + name) or from an id that is only unique per tenant;
- object-storage keys from the uploaded filename (`uploads/avatar.png`) — one tenant overwrites or reads another's;
- search document ids, deduplication keys, idempotency keys and lock names without the tenant;
- globally unique constraints that let one tenant squat or probe another tenant's slug, email or domain.

```go
// ❌ every org shares one key space
key := fmt.Sprintf("dashboard:%s", req.Name)
// ✅
key := fmt.Sprintf("org:%s:dashboard:%s", auth.OrgID(ctx), req.Name)
```

## 3. Signed URLs and Shared Links

- A presigned URL that allows more than the issuer could do: `PUT` where `GET` was meant, a key prefix instead of one object, a content type the client chooses, a week of validity for a one-time download.
- A share token that unlocks a folder when it was issued for one file, or keeps working after the item was unshared or its ACL changed.
- A link id that is the object id, so any authenticated user with the id gets the object (access-control-and-idor).

## 4. Derived Copies That Forget the ACL

- **Search and vector indexes:** the filter is applied at ingestion but not at query time, or permission changes never reach the index (llm-and-agent-security §6 for embeddings and context caches).
- **Caches:** a private response cached under a key without the user; an ACL change that does not invalidate the cached decision or object.
- **Previews and thumbnails:** generated into a public bucket or behind a guessable path while the original is private.
- **Logs, traces, analytics, crash reports:** private content or credentials sent to a store with a broader audience or tenant mixing (secrets-and-crypto-review for the credential case).
- **Notifications and emails:** a digest built from the list of everything, not the list the recipient may see.

## 5. Export, Backup, Import, Restore

- **Export scope:** each item is authorized after selection, not only the request; deleted, draft and private fields are excluded; the download of the finished file is authorized again (the export id is not a bearer token).
- **Import and restore:** archive contents are untrusted input. Owner ids, roles, statuses and foreign keys from the file are rewritten to the importer's scope; an import that can overwrite existing records checks write access to each one.
- **Restore and undo:** restoring a snapshot can bring back a removed membership, a revoked key or a permission that current policy no longer allows — re-authorize restored state.

## 6. Migrations and Backfills

```sql
-- ❌ the new column makes every existing document public
ALTER TABLE documents ADD COLUMN is_public boolean NOT NULL DEFAULT true;
-- ✅ existing rows keep their current exposure; new rows opt in
ALTER TABLE documents ADD COLUMN is_public boolean NOT NULL DEFAULT false;
```

Also: a backfill that assigns orphaned rows to a default organization; a nullable new `tenant_id` read with `OR tenant_id IS NULL` (every tenant sees the legacy rows); dual-read code where the old and the new path apply different defaults during rollout; an id format change that lets old and new ids collide.

## 7. Deletion and Revocation

- **Soft-delete bypass:** the default scope hides deleted rows, but a direct lookup, a relation, a search, a background job or an `Unscoped()` path returns or acts on them.
- **Re-registration:** a deleted username, email or domain can be claimed by someone else while old references (shares, invites, webhooks, SSO mappings) still point at it.
- **Stale authority:** removing a member, downgrading a role or revoking a key does not end existing sessions, cached permission decisions, WebSocket subscriptions, scheduled jobs or issued signed URLs.
- **Queued work after delete:** a retry or a queued export recreates or re-sends data that was deleted after it was enqueued.

## Severity Guide

Reading or changing another tenant's data through any copy → HIGH; CRITICAL when unauthenticated or across every tenant. Revoked access that keeps working → HIGH when it grants writes or secrets, MEDIUM for reads of non-sensitive data. A migration that exposes existing private records → HIGH. Retention longer than a privacy preference, with no unauthorized reader → not a finding.

## Common Mistakes

- Reporting a missing `tenant_id` column on a table every path reaches through a scoped repository — read the repository.
- Treating a privacy preference or retention wish as an access boundary.
- Reporting cache keys without the tenant when the cached value is public or the id is globally unique and the ACL is checked after the read.
- Asserting what an external cache, CDN, replica or backup does — when the policy is not in the repository and it decides the finding, it is a needs-validation note (source-visibility-needs-validation).

## Red Flags

- `Unscoped()`, `unscoped`, `BYPASSRLS`, a service-role key or a "system" repository reached from a request handler.
- `SECURITY DEFINER` on a function that reads tenant data.
- A cache, object or lock key built without the tenant from a client-chosen value.
- An import that inserts the file's `owner_id`, `org_id`, `role` or `status` as given.
- A delete or revoke handler that updates one table and touches no session, cache, index or job.
- A new boolean visibility column defaulting to `true`.

## References (names and links only)

[OWASP Top 10:2025 A01 Broken Access Control](https://owasp.org/Top10/2025/) · [ASVS 5.0 V8 Authorization, V14 Data Protection](https://github.com/OWASP/ASVS/tree/master/5.0/en) · CWE-212, 359, 613, 639, 863 · [PostgreSQL row security policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html) · [Cloudflare security-audit-skill](https://github.com/cloudflare/security-audit-skill)
