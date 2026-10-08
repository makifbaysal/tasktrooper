---
name: production-engineering-practices
category: quality
description: Error-handling, security, logging and performance bar for production code. Use when writing or changing production code, before the run ends.
---
# Production Engineering Practices

## Overview

Acceptance criteria describe the happy path. Production is the unhappy paths: bad input, failed dependencies, concurrent access, hostile users. This skill is the non-negotiable bar every change clears in addition to its AC — the things a reviewer at code_review will bounce you for even when the feature "works."

**Core principle:** Code isn't done when it works; it's done when it fails safely, leaves a trace, and can't be abused.

## The bar

### Error handling
- **No swallowed errors.** Every error is either handled explicitly or propagated with context. An empty catch/`if err != nil { }` that continues is a review-blocking defect.
- Wrap with context at each layer so the log names where it broke: `fmt.Errorf("create task: %w", err)`.
- User-facing errors are actionable and safe; internal detail (stack, ids) goes to logs, never to the client.

### Logging & observability
- Log at the failure site with structured fields (entity id, operation, not free-text).
- **Never log secrets, tokens, passwords, or full request bodies** that may contain them.
- Log the decision, not the novel — one structured line beats ten prose lines.

### Security
- Validate and bound every external input at the boundary: length, type, range, allowed set. Reject early.
- Secrets come from config/env — never hardcoded, never committed, never logged.
- A new endpoint/route gets the SAME auth/authorization guard as its neighbors. Copy the guard, don't omit it.

### Performance
- No N+1 queries or requests — batch, join, preload, or cache. A loop issuing one call per row is a defect.
- Bound every result set: pagination or explicit limits on list endpoints and list views.
- Don't load unbounded data into memory; stream or page.

## Per stack

| Stack | Adds |
|-------|------|
| **Backend** | Parameterized queries only — no string-concatenated SQL, ever. |
| **Web** | No `dangerouslySetInnerHTML` / `innerHTML` with unsanitised data. Nothing secret in `VITE_*` / `NEXT_PUBLIC_*` — those ship to the browser. Every fetch has error and timeout/abort handling, and every route has an error boundary. No token in `localStorage` unless the repository already does it that way. |
| **Mobile** | Tokens in Keychain/Keystore (`flutter_secure_storage`), never in plain prefs. Explicit offline and timeout paths — a request with no network is a state, not a crash. No PII in logs. Permissions requested at the point of use, not on launch. |

## Quick self-review before code_review

| Check | Pass condition |
|-------|----------------|
| Errors | None swallowed; all wrapped with context |
| Input | Every external field validated and bounded |
| Secrets | None in code, logs, or commits |
| Auth | New endpoints/routes guarded like neighbors |
| Queries/requests | No N+1; result sets bounded |
| Tests | Behavior change ships with a test in this task |

## Worked Example

Adding `GET /projects/:id/tasks`. The AC just says "return the project's tasks." The production bar adds:
- Validate `:id` is a UUID → 400 on garbage, before any DB call.
- The query filters by `project_id` with a bound parameter and a `LIMIT`/offset — not `SELECT * FROM tasks`.
- The handler reuses the project's auth middleware so a user can't read another tenant's tasks.
- A structured log line on the DB error path with `project_id`.
- Tests: happy path, invalid id → 400, and the pagination bound.

The feature "worked" after the first bullet; it was *done* after all five.

## Handoff

See your prompt's closing step for how to end the run — this skill only sets the bar the diff clears before you get there.

## Red Flags

- "I'll add validation/error handling later" — later is the review bounce.
- A `catch`/`if err != nil` block that does nothing.
- A list endpoint or list view with no limit.
- Copying an endpoint or screen but dropping its auth guard.
- A secret read from `VITE_*`/`NEXT_PUBLIC_*`, or a token written to plain prefs on mobile.
