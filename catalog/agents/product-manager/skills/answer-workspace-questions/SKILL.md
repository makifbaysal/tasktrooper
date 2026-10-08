---
name: answer-workspace-questions
category: pm
description: Use when the stakeholder asks a factual or status question about the workspace - call the matching read tool and answer conversationally, without creating tasks or asking for approval
---
# Answer Workspace Questions

## Overview

A factual question deserves a factual answer, not a plan. The failure mode is treating "how many projects do we have?" as work to orchestrate — creating tasks, asking approval, or claiming there's "no active context." Just call the read tool and answer.

**Core principle:** Read tool first, then answer in one or two sentences with the real data.

## The read tools

| Question | Tool |
|----------|------|
| How many projects / what are they | `list_projects` |
| What repositories exist / which project each belongs to | `list_repositories` |
| Who is on the team / which role does what | `list_team` |
| What's on the board / a task's status | `list_board_tasks` |
| Overall status / "how are we doing" | `get_board_summary` (aggregated by column/type/priority) |
| What's next / what's ready to start | `list_ready_tasks` |

These read tools are **always available and never need an active repository** — never answer "no active project/repo context." Call the tool and use the real data.

## Rules

1. Call the matching read tool FIRST.
2. Prefer `get_board_summary` for "status/how are we doing" over listing every task — it returns the total and counts grouped by column, type and priority in one call. Only drill into `list_board_tasks` when the stakeholder asks about a specific task or you need titles/assignees.
3. Answer conversationally with actual numbers/names — e.g. "We have 2 projects right now: Alpha and Beta."
4. Translate counts into an outcome update, don't just recite them: what's in progress, what's waiting (backlog/todo), what's blocked or in review (need_revision/pm_uat), and the biggest bucket (see stakeholder-communication).
5. Do NOT create tasks, ask for approval, open analiz, or describe your internal plan for a plain question — just answer like a human PM.

## Worked Example

Stakeholder: "How many repos do we have?" → call `list_repositories` → "We have 3 repos: backend-api, web, and mobile."
Stakeholder: "Who is on the team?" → call `list_team` → answer from what it returns, e.g. "system-architect and security-agent review, backend, frontend, mobile, data and game developers build, qa tests — each one owns its own area."
Stakeholder: "How are we doing?" → `get_board_summary` → {total 24; in_progress 3, code_review 2, need_revision 4, backlog 10, done 5} → "24 tasks in total. 3 are in active development, 2 are in architecture review. 4 went back for revision — that's where the biggest risk is. 10 are queued and 5 are done." Not a wall of 24 task titles.

## Common Mistakes

- Answering from memory instead of calling the tool.
- "No active project context" — the read tools never need one.
- Turning a plain question into an orchestration plan.
- Paging `list_board_tasks` for a status question `get_board_summary` answers in one call.
- Reporting raw counts with no interpretation or risk callout.

## Red Flags

- You created a task in response to a factual question.
- You answered a count without calling a read tool.
- A status update with no mention of the blocked/revision bucket.
