# TaskTrooper vs bb

*Extensible agent IDE · [getbb.app](https://getbb.app) · License: MIT*

An agent IDE built from plugins, driven from a desktop app, the web, a CLI or an API.

bb runs agents in threads you can follow live, steer at any turn or hand to another agent, on one machine or several. Almost everything is a plugin, including the task board, workflows and account handling, and every feature is also a bb CLI command. TaskTrooper is narrower and more opinionated: a fixed board of role agents that takes a task from analysis to production on one machine, with nothing to enroll or host.

| | bb | TaskTrooper |
|---|---|---|
| **Where it runs** | A local server plus a host daemon on each machine you enroll. Desktop app for macOS and Linux, Windows through WSL2, a web app and an iOS app in early access. Remote access through bb connect or Tailscale. | On your machine: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted, no account. |
| **Who starts the work** | You, per thread, from the app, the CLI, scripts or cron. A workflows plugin runs orchestration scripts that agents write. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | None built in. A thread can be a manager that coordinates other threads. | Seven role agents (PM, architect, backend, frontend, mobile, QA, release engineer) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. |
| **Task board** | A tasks plugin with a board view; not part of the core. | The core of the product: thirteen columns, each owned by a role agent. |
| **Isolation** | Environment plugins: a git worktree, the live checkout, a scratch folder or a Modal cloud sandbox. | Each task gets its own local clone of the repository on its own branch and its own CLI session. Three sessions run at once by default. |
| **Extending it** | Plugins with a marketplace; agents can edit and reload bb's own plugins. Every feature is also a CLI command with JSON output. | Agents, skills, rules, tool policies and columns are edited in the app and synced from the catalog. No plugin system. |
| **QA** | None built in. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | None. | The release engineer merges, deploys through the component's delivery profile (Cloud Run, GKE, ECS/Lambda, Vercel, Fly), verifies production and rolls back. Incidents from Alertmanager, Sentry or webhooks; App Store and Play releases. |
| **Agent runtimes** | Claude Code through its Agent SDK, Codex through its app server, Cursor, OpenCode, Grok and others over ACP, and Pi. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API, including Ollama and LM Studio. |
| **Usage limits** | Plugins retry a turn at the provider's reset time and can rotate between several accounts. | A task that hits a usage limit parks on Blocked with its resume time, other runs on that CLI are held, and the task continues at the reset (Claude Code sessions resume with --resume). Unattended, across the whole board. |
| **Data** | Local server. Anonymous usage counts, opt-out; bb connect is a hosted pairing relay. | Stays on the machine. No account, no relay. Official builds send one anonymous active-install count, opt-out ([details](../data-and-security.md#anonymous-usage-statistics)). |
| **Price** | Free, MIT. | Free, Apache-2.0. You pay your own model or CLI subscription. |

## Choose bb if

- You want to reshape the tool itself with plugins and scripts.
- You run agents on several machines, or want a CLI and API for everything.
- You want to steer a running thread turn by turn.

## Choose TaskTrooper if

- You want a board of roles that works without you steering each thread.
- You want QA, merge and deploy in the same loop as the code.
- You want one desktop app with nothing to enroll, pair or host.

---

bb is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/bb](https://tasktrooper.ai/compare/bb).
