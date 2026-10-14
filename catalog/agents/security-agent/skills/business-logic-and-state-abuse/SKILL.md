---
name: business-logic-and-state-abuse
category: security
description: Use when the diff changes a workflow with states, money, credits, quotas, approvals, invites, coupons, counters or expiry - skipped or replayed steps, check-then-act races with real impact, negative or overflowing quantities, partial-failure rollback, fail-open defaults, and legitimate features (export, import, search, preview, invite) used against their own rules
tech_stack: Web & API
source: cloudflare/security-audit-skill (MIT) business-logic, feature-abuse and chained-trust attack classes, adapted to diff review; own wording; OWASP Top 10:2025 A06 Insecure Design and CWE cited by name only
---
# Business Logic and State Abuse

## Overview

Logic flaws have no dangerous-looking line: no `exec`, no string-built query, nothing a scanner matches. Every request in the attack is valid on its own; the vulnerability is the sequence, the timing or the number. They are also where the money is — free credits, double refunds, skipped approvals, a role nobody granted.

**Core principle:** name the invariant the workflow promises ("an order ships only after it is paid", "a coupon is redeemed once", "an invite cannot grant more than the inviter has"), then look for a sequence of individually valid requests that breaks it.

## 1. State Machines — Skipping, Reversing, Replaying (CWE-841)

```go
// ❌ the transition never checks the current state: ship an unpaid order, refund a refunded one
func (s *Service) Ship(ctx context.Context, id string) error {
	return s.repo.SetStatus(ctx, id, StatusShipped)
}

// ✅ the precondition is part of the write; zero rows means the transition is not allowed
res, err := s.db.ExecContext(ctx,
	`UPDATE orders SET status = 'shipped' WHERE id = $1 AND org_id = $2 AND status = 'paid'`, id, orgID)
if n, _ := res.RowsAffected(); n == 0 { return ErrInvalidTransition }
```

Look for: a status taken from the request body; a step endpoint that does not check the previous step completed (verify-email, MFA, payment, approval, KYC); a completed flow that can be replayed (redeem, claim, confirm, accept-invite); a "cancel" that leaves the side effect (credits granted, access given) in place.

## 2. Check-Then-Act Races With Real Impact (CWE-362, CWE-367)

Not a "theoretical race": here the attacker sends both requests. A finding when the window is one read-then-write in a handler and the impact is money, quota, a unique claim or a privilege.

```ts
// ❌ two parallel requests both read balance=100 and both withdraw 100
const acct = await db.account.findUnique({ where: { id } });
if (acct.balance < amount) throw new Error("insufficient");
await db.account.update({ where: { id }, data: { balance: acct.balance - amount } });

// ✅ one atomic conditional write
const n = await db.$executeRaw`UPDATE account SET balance = balance - ${amount}
  WHERE id = ${id} AND balance >= ${amount}`;
if (n === 0) throw new Error("insufficient");
```

Fixes that work: a conditional `UPDATE … WHERE`, `SELECT … FOR UPDATE` inside a transaction, a unique constraint (one redemption row per coupon and user), an idempotency key stored with the result. Fixes that do not: an in-process mutex in a horizontally scaled service, a check in the client, "the UI disables the button".

## 3. Numbers — Negative, Zero, Huge, Imprecise (CWE-20, CWE-190, CWE-681)

- **Sign:** `quantity: -5` turns a purchase into a refund; a negative transfer moves money the other way; a negative duration extends a trial.
- **Zero and empty:** a zero-price item, a zero-length array that skips the per-item check, `limit=0` meaning "unlimited".
- **Overflow and width:** `price * qty` in 32-bit ints; a total stored in a narrower column than it is computed in; JavaScript numbers above 2^53.
- **Precision:** money in floats; rounding applied per line in one place and on the total in another, repeatable for a cent each time.
- **Coercion:** `"1e9"`, `" 5 "`, `"0x10"`, `true` reaching a numeric field through a loose parser.
- **Client-supplied value:** price, discount, currency, tax or plan sent by the client and trusted by the server.

## 4. Partial Failure and Rollback

Step 1 commits and step 2 fails: credits granted but the charge declined, a seat allocated but the invite email errored and the retry allocates another, a file stored but the scan that should gate it timed out and the record says `clean`. Look for multi-step side effects outside one transaction, an external call between two writes, and a `catch` that logs and continues past a security-relevant step. Compensation must run with the same authorization as the original action.

## 5. Time-Based Logic

- Expiry compared with a client-supplied timestamp, or with `<` where `<=` is meant at a boundary that matters (a token valid one extra window).
- Time zones: a "daily" limit that resets per request time zone; a trial end computed in the client's zone.
- Scheduled work that uses the state at scheduling time instead of re-checking at execution (a removed member's scheduled export still runs as them).

## 6. Defaults, Fallbacks and Fail-Open Posture

What does the code do when the config key is missing, the feature flag is off, the policy service times out, or the record predates the migration?

```python
# ❌ a missing plan means "no limits"
limit = PLAN_LIMITS.get(org.plan)
if limit is not None and usage >= limit: raise QuotaExceeded()
# ✅ unknown plans get the most restrictive limit
limit = PLAN_LIMITS.get(org.plan, PLAN_LIMITS["free"])
```

Fail-open authorization is access-control-and-idor §5; here the same question applies to entitlements, quotas, approval requirements and review gates.

## 7. Features Used Against Their Own Rules

- **Invites and sharing:** an inviter grants a role above their own; a share link carries edit rights the sharer lacks; accepting an invite to one workspace joins another by id swap.
- **Export and backup:** includes fields, drafts, deleted rows or other members' items above the requester's access (data-isolation-and-lifecycle §5).
- **Import and restore:** recreates records with owner, role or status fields taken from the file.
- **Search, filter and sort as an oracle:** filtering on a field the user cannot read (`?filter=salary>100000`), sorting by a hidden field and watching the order, a count that reveals existence.
- **Approvals:** the approval is recorded for one amount, recipient or diff and the execution uses another (bind the approval to the exact object, and invalidate it when the object changes).
- **Entitlements enforced only in the UI:** a paid feature, seat count or plan limit checked in the client and not on the server endpoint.

## 8. Chains Across Steps

Individually harmless outputs can be the prerequisite of a later trust decision: a self-service rename that produces a name another component treats as an admin group, an "unverified" email that a later SSO link trusts, a draft visible to its author that a preview token then exposes to everyone. Write each step's output and the later decision that relies on it; report only when every link is shown in source.

## Severity Guide

Unpaid goods, free credits, double refunds or a role nobody granted → HIGH; CRITICAL when unauthenticated or unbounded. Bypass of a paid entitlement or quota → MEDIUM to HIGH by value. An oracle that reveals a protected value or record → MEDIUM. A user doing something the product allows them to do → not a finding.

## Common Mistakes

- Reporting a race without writing down the two concurrent requests and the state they leave behind.
- Reporting "no rate limit" on redeem or login — excluded; the double-redeem race is the finding, not the missing limit.
- Calling a UI-only constraint a vulnerability without reading the server endpoint that enforces it.
- Calling an intended product rule ("owners can delete any task") a logic flaw.

## Red Flags

- A status, price, discount, role or plan read from the request body.
- `find` then `update` of a balance, counter or stock level with no lock, condition or constraint.
- A `default:` branch, `?? true`, `getOrDefault(…, true)` or `if err != nil { return nil }` on an entitlement or approval check.
- A side effect performed before the check that should gate it, or a `catch` that continues past it.
- An invite, share or role-assignment endpoint that never compares the granted role with the granter's.

## References (names and links only)

[OWASP Top 10:2025 A06 Insecure Design](https://owasp.org/Top10/2025/) · [OWASP API Security Top 10 2023](https://owasp.org/API-Security/editions/2023/en/0x11-t10/) API6 Unrestricted Access to Sensitive Business Flows · CWE-20, 190, 362, 367, 681, 841 · [Cloudflare security-audit-skill](https://github.com/cloudflare/security-audit-skill)
