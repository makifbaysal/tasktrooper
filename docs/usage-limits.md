---
title: Usage limits and concurrency
description: What happens when Claude Code hits its subscription limit, how many sessions run at once, and how a parked task shows on the board.
---

Agents on the Claude Code provider run on your Claude subscription, not a
metered API key — which means they are subject to that subscription's own
usage limit, the same one you would hit using Claude Code yourself from a
terminal. TaskTrooper is built to spend that limit deliberately rather than
waste it retrying into a wall.

## What happens when the limit is hit

Claude Code reports a spent usage limit as a structured event
(`rate_limit_event`) carrying a status, the reset time, and which window was
hit, rather than just an error string. TaskTrooper reads that structured
event first and only falls back to parsing text when it is missing.

- **A board task parks.** The task moves to the **Blocked** column with the
  resume time recorded on it, rather than failing — there is nothing to fix
  and nothing to usefully retry right now, so treating it as a failure would
  spend one of that task's limited consecutive-failure attempts on a billing
  window instead of a real problem.
- **The session resumes, it doesn't restart.** The parked run keeps its CLI
  session id, so when it comes back it resumes that same session
  (`--resume`) with whatever it had already read and written, rather than
  paying to re-explore the repository from scratch.
- **The whole executor gates, not just the one session.** One session
  hitting the limit arms an executor-wide gate: every other board run on
  Claude Code parks immediately without even spawning a new CLI process,
  because the limit is shared across every session on the account. Any
  session that completes successfully afterward clears the gate again.
- **In chat, there is no card to park.** A chat turn that hits the limit
  becomes a message to the person typing, naming the local time it renews
  and suggesting they move the agent to an API-backed provider if they don't
  want to wait — there is no sweeper watching a conversation the way there
  is for a board task.

## When the reset time is missing

If Claude Code's own response didn't include a reset time, TaskTrooper
guesses **30 minutes** and waits that long before trying again — deliberately
short, since guessing low costs one wasted CLI start if the limit is still in
force, while guessing long leaves a task idle for hours after the limit
actually cleared. If that guess is wrong again, the next wait doubles (30m →
1h → 2h → 4h → 5h, capped there, matching how long a subscription's own
usage window actually runs) instead of hammering the same wall every half
hour.

## Concurrency caps

How many board runs start at once is set in **Settings → Concurrency
limits**: the most agent runs and the most distinct tasks running at the same
time. `0` in either field means unlimited, and that is the default.

| Setting | Default | What it limits |
|---|---|---|
| Settings → max concurrent agents / tasks | 0 (unlimited) | How many board runs, and how many different tasks, run at the same time |
| `claude_code.max_concurrent_sessions` | unset (no cap) | An optional host-wide cap on Claude Code sessions, on top of the Settings limits |
| `orchestration.max_parallel_tasks` | 3 | How many subtasks inside one chat orchestration plan run in parallel |

Every session on one Claude subscription shares its usage limit, so many
sessions in parallel reach that limit together, sooner, without getting more
work done. If that happens often, lower the agent limit in Settings. A task
still runs one agent at a time whatever the limits say: its runs share one
workspace, so a second reviewer on the same task waits for the first.

## 429 and 529 pacing for API providers

An agent on an HTTP provider (OpenAI, Anthropic, Gemini, Groq, or a custom
endpoint) hits a different kind of limit — a `429` (rate limited) or `529`
(Anthropic's "overloaded", not billing-related) response — and that is paced
rather than parked: retried with backoff starting around half a second and
doubling up to an 8-second cap, honoring the provider's `Retry-After` header
when it sends one. `529` is treated exactly like `429` for pacing purposes,
but is never reported as the account having run out — an overloaded API
says nothing about how much of your quota is left.

## How it shows on the board

A parked task shows its blocking reason directly on the card and in its
detail view. For a Claude Code usage limit, that reads **"Claude usage limit
reached"**; the same mechanism is what shows a mobile device wait ("Waiting
for a test device"), a pending deploy ("Waiting for the deploy"), or a task
waiting on another task it depends on ("Waiting for blocking tasks") — a
usage-limit park is one instance of a more general "this task is blocked on
something outside any agent's control, and will resume on its own" state, not
a failure requiring your attention. Nothing here needs you to intervene
unless you would rather not wait — at which point moving that agent to an
API-backed provider, from its Settings tab, is the way out mentioned in the
chat-side message above.

## Compared with Claude Code on its own

An interactive Claude Code session now waits when the limit is reached and
continues automatically at the reset time ("Usage limit reached · continuing
automatically at 2:30pm · esc to cancel"). That covers the one terminal you
are watching. A headless `claude -p` run, which is what a board task is, does
not get that; and even in the terminal, nothing else moves while it waits.

TaskTrooper handles the limit for every running task at once: each task parks
on Blocked with its resume time, other runs are held instead of hitting the
same wall, and each session comes back with `--resume` when the limit resets.
No terminal has to stay open.
