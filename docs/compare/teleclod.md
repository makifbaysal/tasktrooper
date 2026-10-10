# TaskTrooper vs Teleclod

*Agent session manager · [teleclod.com](https://teleclod.com) · License: Proprietary*

A dashboard for many Claude Code and Codex sessions that you run from your phone.

Teleclod runs one agent session per task across your projects. You describe the task, an agent returns a short plan, you approve it, and the result is reviewed against your rules before anything is committed. Its focus is supervision from anywhere: a phone app, a Chrome sidebar, voice and email. TaskTrooper focuses on what happens after the plan: role agents, QA, merge and deploy, on your machine with no account.

| | Teleclod | TaskTrooper |
|---|---|---|
| **Where it runs** | Desktop app for Mac and Windows. Phone and browser access through Teleclod's relay; a Docker image for an always-on host. | On your machine: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted, no account. |
| **Who starts the work** | You describe a task, also by email or voice. An agent plans it and waits for your approval. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | One agent per task. You can pick a different model for planning, execution and review. | Seven role agents (PM, architect, backend, frontend, mobile, QA, release engineer) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. |
| **Board** | Columns are projects, cards are tasks. | Columns are stages, each owned by a role agent. |
| **Review** | The agent checks its work against rules you set and reports findings by severity; you approve before the commit. | Every task opens its own pull request. Code review reads it, and every acceptance criterion needs a verdict before the card can leave a review column. |
| **QA** | The review against your rules. A Chrome extension lets agents check pages in your open tabs. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | Not part of the product. | The release engineer merges, deploys through the component's delivery profile (Cloud Run, GKE, ECS/Lambda, Vercel, Fly), verifies production and rolls back. Incidents from Alertmanager, Sentry or webhooks; App Store and Play releases. |
| **Agent runtimes** | Claude Code, Codex, Gemini and Kimi CLIs plus API providers, with your own keys. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API, including Ollama and LM Studio. |
| **Data** | Project data in a local encrypted SQLite. Remote access goes through Teleclod's relay and needs an account; usage analytics, opt-out. | Stays on the machine. No account, no relay. Official builds send one anonymous active-install count, opt-out ([details](../data-and-security.md#anonymous-usage-statistics)). |
| **Price** | Free tier, Pro €49/month, Studio €99/month; 14-day trial. | Free, Apache-2.0. You pay your own model or CLI subscription. |

## Choose Teleclod if

- You want to approve plans and answer agents from your phone.
- You want to hand over tasks by voice or email.
- You want an always-on runner in Docker.

## Choose TaskTrooper if

- You want the code to stay on one machine, with no account or relay.
- You want QA, merge and deploy handled by agents, not only the plan and the code.
- You want it free and open source.

---

Teleclod is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/teleclod](https://tasktrooper.ai/compare/teleclod).
