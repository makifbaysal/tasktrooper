# TaskTrooper vs Modula

*Agent pipeline · [modula.sh](https://modula.sh) · License: Elastic License 2.0*

An event-driven pipeline that turns tickets into code with a team of agents.

Modula is the closest in spirit. Tickets move through a pipeline of agents (project manager, researcher, worker, code reviewer, reviewer); each agent starts when its rule matches an event, and a human approves the task and picks the result. It can run several solution variants of one task in separate worktrees. It stops at the merge. TaskTrooper carries the task on through QA, deploy and production, and runs API models as well as agent CLIs.

| | Modula | TaskTrooper |
|---|---|---|
| **Where it runs** | Desktop app (Tauri) with a background engine and a SQLite database under ~/.modula. | On your machine: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted, no account. |
| **Who starts the work** | Agent rules. Every change is an event, and each agent's rule decides whether it starts. A human approves a task before it enters the pipeline. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | Five pipeline agents (project manager, researcher, worker, code reviewer, reviewer) plus scan agents for Jira, Linear and GitHub, with editable prompts and rules. | Seven role agents (PM, architect, backend, frontend, mobile, QA, release engineer) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. |
| **Parallel solutions** | Up to ten variants of one task, each on its own branch and worktree. A reviewer agent compares them and a human picks the winner. | One solution per task, on its own branch and pull request. |
| **Issue trackers** | Scheduled scan agents mirror Jira, Linear and GitHub issues into tasks. | GitHub for pull requests, reviews and CI status. No built-in Jira or Linear intake. |
| **Review** | A code reviewer agent per variant, a reviewer agent across variants, then an in-app diff for the human. | Every task opens its own pull request. Code review reads it, and every acceptance criterion needs a verdict before the card can leave a review column. |
| **QA** | No test role; the worker runs what it runs. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | None. A human merges and marks the task accepted. | The release engineer merges, deploys through the component's delivery profile (Cloud Run, GKE, ECS/Lambda, Vercel, Fly), verifies production and rolls back. Incidents from Alertmanager, Sentry or webhooks; App Store and Play releases. |
| **Agent runtimes** | Claude Code, Codex, OpenCode and Gemini CLI, each started with its permission-bypass flag. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API, including Ollama and LM Studio. |
| **Data** | Local, no telemetry. Remote access is a closed-source plugin. | Stays on the machine. No account, no relay. Official builds send one anonymous active-install count, opt-out ([details](../data-and-security.md#anonymous-usage-statistics)). |
| **Price** | Free to use and self-host under the Elastic License 2.0, which does not allow offering it as a hosted service. | Free, Apache-2.0. You pay your own model or CLI subscription. |

## Choose Modula if

- You want several competing solutions for each task and to pick one.
- Your work starts as Jira or Linear tickets that should be pulled in on a schedule.
- You want to wire agent triggers yourself with rules.

## Choose TaskTrooper if

- You want the task to reach production: QA, merge, deploy, rollback.
- You want a QA agent that has to run the code before a task passes.
- You want API models and more agent CLIs on the same board, under Apache-2.0.

---

Modula is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/modula](https://tasktrooper.ai/compare/modula).
