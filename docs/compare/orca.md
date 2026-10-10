# TaskTrooper vs Orca

*Agent IDE · [www.onorca.dev](https://www.onorca.dev) · License: MIT*

An IDE that runs many coding agents side by side, each in its own git worktree.

Orca is a desktop IDE built around the git worktree. Every task gets its own checkout, and Claude Code, Codex and some thirty other agent CLIs run next to each other in terminals, with an editor, a browser and a diff view beside them. You create the worktree, pick the agent, write the prompt and review what comes back. TaskTrooper starts from the other end: the board hands each card to the agent that owns its column, and the task keeps moving through QA, merge and deploy without you queuing each step.

| | Orca | TaskTrooper |
|---|---|---|
| **Where it runs** | Desktop app for macOS, Windows and Linux. Optional SSH hosts, a remote Orca server, and iOS/Android companion apps that pair through Orca's end-to-end encrypted relay. | On your machine: the desktop app starts an embedded Postgres, the Go backend and the agent sessions. Nothing hosted, no account. |
| **Who starts the work** | You: create a worktree, choose an agent, write the prompt. An experimental orchestration mode lets one agent dispatch tasks to others. | The board. A card entering a column is dispatched to that column's agent; nobody presses run. |
| **Roles** | None built in. Any agent can take any task; roles exist only if a coordinator agent assigns them. | Seven role agents (PM, architect, backend, frontend, mobile, QA, release engineer) with seeded skills, rules, tool policies and memory. Add your own from a template or from scratch, and give each agent its own runtime: one board can mix Claude Code, Cursor, OpenCode, Antigravity and API models. |
| **Isolation** | One git worktree per task, with gitignored folders such as node_modules shared into it. | Each task gets its own local clone of the repository on its own branch and its own CLI session. Three sessions run at once by default. |
| **Review** | In the app: a diff view where you comment on lines and send all comments back to the agent as one prompt. | Every task opens its own pull request. Code review reads it, and every acceptance criterion needs a verdict before the card can leave a review column. |
| **Lifecycle** | Create, work, review, then commit, push and open the pull request from the app. | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy. |
| **QA** | Whatever the agent runs. An embedded browser lets you or the agent check the UI. | A QA agent that cannot pass a task without executing: real requests, headless browser, iOS/Android simulators. |
| **After merge** | None. The workflow ends with the pull request and its checks. | The release engineer merges, deploys through the component's delivery profile (Cloud Run, GKE, ECS/Lambda, Vercel, Fly), verifies production and rolls back. Incidents from Alertmanager, Sentry or webhooks; App Store and Play releases. |
| **Agent runtimes** | 30+ agent CLIs, including Claude Code, Codex, Gemini, Cursor, Copilot, OpenCode and Pi. Agents start with their permission-bypass flag unless you switch them to manual. | Claude Code, Cursor, Antigravity and OpenCode as local processes; Anthropic, OpenAI, Gemini, Groq or any OpenAI-compatible API, including Ollama and LM Studio. |
| **Data** | Local. Mobile pairing goes through Orca's relay; anonymous usage analytics, opt-out. | Stays on the machine. No account, no relay. Official builds send one anonymous active-install count, opt-out ([details](../data-and-security.md#anonymous-usage-statistics)). |
| **Price** | Free, MIT. | Free, Apache-2.0. You pay your own model or CLI subscription. |

## Choose Orca if

- You want an IDE: terminals, editor, browser and diffs around several agents at once.
- You want to watch and answer agents from your phone.
- You work across SSH hosts or a remote build machine.

## Choose TaskTrooper if

- You want the board to start the work, not you.
- You want QA, merge and deploy to follow the code without queuing them yourself.
- You want roles with their own tools, and acceptance criteria that gate every card.

## Together?

Loosely. Every TaskTrooper task ends up on its own branch and pull request, which you can open in an Orca worktree to inspect or finish by hand.

---

Orca is described from its public documentation. If something here is out of date, open an issue. The same page is on [tasktrooper.ai/compare/orca](https://tasktrooper.ai/compare/orca).
