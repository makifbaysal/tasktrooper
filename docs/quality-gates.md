---
title: Quality gates
description: What has to be true before a task can move forward, and what happens when an agent needs a human answer.
---

TaskTrooper gates every hand-off between review stages on something real
having happened, not on an agent saying it did. This page covers each gate,
in the order a task meets them.

## The criteria gate

A task carrying acceptance criteria cannot leave a review column until
**every criterion has a verdict** from the role reviewing that stage — QA
approving or rejecting each one on the way out of QA, PM approving or
rejecting each one on the way out of PM UAT. This applies to every forward
exit from a review column (into PM UAT, Human UAT, Done or Released); moves
backward into Need Revision are never gated on it. A task with no criteria at
all, or a repository with the criteria requirement turned off, is exempt.

The implementer marking a criterion "completed" is a claim, not a verdict —
see [Tasks, criteria and documents](tasks.md#acceptance-criteria-and-verdicts)
for the distinction and why a criterion needs sign-off from the role actually
reviewing it, not just the person who built it.

## QA must execute

A QA run cannot be marked complete without having actually run something —
a real request, a headless-browser check, a mobile simulator/emulator
interaction. This exists because a run can otherwise write a plausible list
of scenarios "to verify" in the future tense, call nothing but a board move,
and still look complete to anything reading the transcript alone.

Concretely: a run in Ready for QA / In QA that never successfully executes
one of QA's grounding tools is marked **failed** rather than completed, with
the reason posted on the task, and a fresh QA attempt is dispatched (bounded
at three consecutive failed attempts before it's left for a person). Writing
a review comment, moving the card, or reading the pipeline status does not
count as evidence — the verdict can't be its own proof — and neither does
reading code, because QA's job here is black-box. `analiz` tasks, and a run
that ends by asking a clarifying question instead of finishing, are exempt.

## The QA pipeline

The moment a task enters **Ready for QA**, TaskTrooper runs an automatic
build/test pipeline against the task's own workspace in the background,
using the repository's configured verify/build/test commands where set, or
auto-detecting them by ecosystem (Go, Node, Rust, Python, Maven, Gradle) —
QA is only dispatched into In QA once that result is in. A failing pipeline
sends the task straight to Need Revision with the failing stage's log
attached as a comment, rather than letting QA test against a build that
doesn't work.

A repository with no build/test command configured and nothing
auto-detected records the pipeline as **skipped** rather than failed — this
still lets QA proceed (there's nothing wrong to block on), but the card never
shows a green passing badge for it, because nothing was actually compiled or
tested. "Skipped" and "passed" are kept visibly different for exactly that
reason.

## The code-review gate

Moving a task into **Code Review** normally waits for the same build/test
pipeline to report before the reviewing agent is dispatched — reviewing a
diff that doesn't build wastes the reviewer's time. This wait is bounded so
it can never leave a card stuck indefinitely with a spinner and no agent:

| Outcome | What happens |
|---|---|
| A real pipeline result comes back | Success or skipped hands the task to its reviewer; failure sends it to Need Revision with the failing log |
| No CI is configured on the repository | The gate opens anyway — there was never anything to wait for |
| CI is unavailable (billing, quota, outage) | The gate opens after a short grace period rather than waiting on an answer that can't come |
| Nothing reports within the timeout (45 minutes) | The gate opens regardless |

Whenever the gate opens without a real green result, the card records why
(no CI configured, CI unavailable, or timeout) so a warning shows instead of
a pipeline badge that looks like it passed. Each repository has its own
setting — **require pipeline for review**, on by default — to turn this wait
off entirely if its CI can't reliably answer; with it off, Code Review
dispatches immediately.

## Why the code-review wait can't hang forever

The four outcomes above exist because a review gate that only opens when
something tells it to is one silent failure away from a card stuck forever
with a spinner and no reviewer. TaskTrooper backs the wait with more than
one path so a single broken piece can't strand it: it listens for GitHub's
own webhook when one is registered, repairs that webhook automatically if
it's ever missing or out of date, and separately polls GitHub every couple
of minutes to settle anything the webhook missed. The timeout is the last
resort behind all of that, not the primary mechanism — by the time it fires,
every faster path has already had a chance to answer.

## The deploy dependency gate

A task with `deploy_depends_on` relations does not ship until every one of
those tasks has been released — see [Ordering and
relations](ordering-and-relations.md#deploy_depends_on--shipping-order) for
what that holds back in each delivery mode and how the relation is set. The
task says which keys it is waiting on, and it resumes on its own once they
ship.

## Clarification

When an agent genuinely cannot proceed without information only you have, it
asks rather than guessing. What happens:

1. The agent's run ends by asking one or more questions instead of finishing
   the task.
2. The task moves to **Blocked**, recording the question, which column it
   was working in (so answering returns it there, not to a guess), and the
   chat thread the question was asked in.
3. You answer inside that task's chat thread — the same **Discuss** thread
   described in [Your first task](first-task.md#chatting-with-the-agent-about-a-task).
   Follow-up questions on the same task continue that thread rather than
   opening a new one that can't see what was already asked.
4. Answering clears the block and the task resumes automatically — no manual
   drag back onto a working column needed.

If a clarification comes up during a board run rather than a chat, TaskTrooper
also opens the task's chat thread for you and sends a desktop notification, so
you don't have to go looking for which task is waiting on you.

## The review chain and release-deploy gates

Two further, opt-in checks live on each repository:

- **Require review chain** blocks a move into Done (or into Released when
  that would skip Done) unless the task actually visited every stage its
  type requires — see [Board and columns](board-and-columns.md#the-review-chain-by-task-type)
  for exactly which stages, by type.
- **Require release deploy** blocks a move into Released unless a
  successful production deploy (or a successful staging deploy, on a
  repository with no production workflow mapped) is recorded for the task.
  `analiz` tasks are exempt — they ship no code.

Both default off, because each is only honest on a board actually wired for
it: a repository with no QA agent subscribed anywhere, or no deploy workflow
mapped, could never satisfy either one. Turn them on from a repository's
settings once its board is actually set up to clear them — see [Board and
columns](board-and-columns.md#the-review-chain-by-task-type).

## A note on what's enforced versus what's advisory

Every gate on this page blocks something concrete: a move, a release, a
run being marked complete. Comments, descriptions and the deploy runbook
fields are not enforced the same way — they're read by the people and
agents who need them, but nothing refuses a move because `rollback_plan` is
empty. If a step genuinely has to happen, it belongs in a gate like the ones
above; if it's guidance for whoever handles the next step, it belongs in a
field or a comment instead.

That split is deliberate: a gate that blocked on every field being filled in
would make simple tasks slower for no safety gained, while a genuinely
required check that only lived in a comment would eventually get skipped by
someone in a hurry. Everything in this document exists because skipping it
once produced a real problem — a merged-but-unreviewed change, a released
task nobody actually tested, a review cycle that repeated itself forever
against a build that could never go green.
