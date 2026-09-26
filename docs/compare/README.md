# How TaskTrooper compares

Generated from the site's `content/compare.ts` and `content/matrix.ts` with `node scripts/export-compare.ts`; edit there, not here.

### Local-first peers

Open-source projects that also put coding agents to work on your own machine. The short version: they give you a tracker, a queue or a session manager that you drive; TaskTrooper is the whole loop in one desktop app, and the board drives it.

| | [TaskTrooper](https://github.com/makifbaysal/tasktrooper) | [Beads](https://github.com/steveyegge/beads) | [Beadhive / Gas City](https://beadhive.ai) | [Vibe Kanban](https://github.com/BloopAI/vibe-kanban) | [Claude Squad](https://github.com/smtg-ai/claude-squad) |
|---|---|---|---|---|---|
| What it is | Desktop app: board, role agents and the runtime that runs them | Git-embedded issue tracker and memory for agents (`bd` CLI) | Software factory on top of Beads (`bh` CLI; Gas City orchestrates it) | Kanban and per-task workspaces for coding agents (announced as sunsetting) | TUI that runs many agent sessions side by side |
| Who starts the work | The board: a card entering a column is dispatched to that column's agent | You, or an agent you are already running | Its planner/dispatcher agents, behind human gates | You, per task | You, per session |
| Roles | Six seeded agents (PM, architect, backend, frontend, mobile, QA) with skills and rules that rewrite themselves from results | None, it tracks work for any agent | Planner, dispatcher, developer, reviewer, merger, warden | None | None |
| Your own agents | Create agents from a template or from scratch: prompt, skills, rules, tool policy, columns, memory. Each agent picks its own runtime, so one board mixes Claude Code, Cursor, OpenCode, Antigravity and API models | No agents; bring your own | Roles are configured in the factory; agents run through its CLI | Pick a supported agent per task; no agent definitions | Pick a program per session; no agent definitions |
| Parallel agents and tools | Several tasks at once (three sessions by default), each in its own workspace, branch and CLI session; each role has its own tool policy, so the backend agent runs tests while QA drives a browser and the architect reads a third task's PR. QA has no code tools | Any number of agents share the graph; what each may do is up to the agent you run | Role agents on one graph; tool separation by role | One agent per task, same capabilities | Several sessions side by side, each a full agent |
| Lifecycle | Thirteen columns out of the box: analysis review, code review, QA, PM UAT, human UAT, Done merges, Released watches the deploy | Open/closed with typed dependencies | Plan, review, merge with human gates | Todo, in progress, review | Branch, diff, commit |
| QA | A QA agent that cannot pass a task without running something: real requests, headless browser, iOS/Android simulators | None | Reviewer agents | None | None |
| After merge | Deploy recipes, health checks, incidents from Alertmanager/Sentry/webhooks, rollback, App Store and Play releases | None | Release automation | None | None |
| Work ordering | `blocked_by`, `deploy_depends_on`, `derived_from`, `discovered_from`; a ready queue for agents | `blocks`, `parent-child`, `discovered-from`, `related`; `bd ready` | Beads' graph | None | None |
| Storage | Embedded Postgres in the app's data directory | Dolt under `.beads/`, synced through git | Beads | Local database, or a self-hosted server | tmux sessions and git worktrees |
| Agent runtimes | Claude Code, Cursor, Antigravity, OpenCode as local processes; OpenAI, Anthropic, Gemini, Groq or any OpenAI-compatible API | Any agent that can call a CLI | Any, through its CLI | Claude Code, Codex, Gemini CLI, Copilot, Amp, Cursor, OpenCode and more | Claude Code, Codex, Gemini, Aider |
| Interface | Desktop app (macOS, Windows, Linux) | CLI, plus community UIs | CLI | Web UI | Terminal UI |
| License | Apache-2.0 | MIT | See their repositories | Apache-2.0 | AGPL-3.0 |

### Hosted and single-agent tools

The agent products most people already use. Several of these are what TaskTrooper runs underneath rather than rivals: it runs on an agent CLI you already have (Claude Code, Cursor, Antigravity or OpenCode) or on a plain API key, and it reads the PRs and CI these tools produce.

| | [TaskTrooper](https://github.com/makifbaysal/tasktrooper) | [Claude Code](https://claude.com/claude-code) | [Cursor Cloud Agents](https://cursor.com/docs/cloud-agent) | [Devin](https://devin.ai) | [Copilot coding agent](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) | [OpenHands](https://github.com/OpenHands/OpenHands) | [Xirp](https://xirp.spotify.com/) |
|---|---|---|---|---|---|---|---|
| What it is | Desktop app: board, role agents and the runtime that runs them | Terminal agent; one session you drive | Parallel agents in cloud VMs that open PRs | Cloud AI engineer you assign tickets to | Assign an issue, get a PR from an Actions runner | Self-hosted control center for agents and automations | Agent grounded in your organisation's services and docs |
| Where it runs | Your Mac: embedded Postgres, Go backend, local agent sessions | Your terminal | Cursor's cloud VMs | Cognition's cloud (CLI for local) | GitHub Actions, ephemeral | Local, Docker, remote VM or OpenHands Cloud | Desktop app plus a Portal plugin; beta |
| Who starts the work | The board, by column | You, per prompt | You, from Cursor, Slack, GitHub, Linear | You, by ticket, Slack or web | You, by issue or @copilot; schedules | You, or an automation on schedule/webhook | You, in a session |
| Roles | Six role agents with skills, rules, tool policies and memory | One agent per session | One agent kind | One Devin, many sessions | One agent | Agents by profile, no role model | One agent |
| Your own agents | Create agents from a template or from scratch; each picks its own CLI or API model, so one board mixes runtimes | Subagents and skills inside one CLI | Rules files; one agent kind | One Devin | One agent | Agent profiles per server; ACP agents | One agent |
| Parallel agents and tools | Three sessions by default, each in its own workspace and CLI session, each role with its own tool policy; QA has no code tools | More terminals; every session can do everything you allow | As many agents as you start, same capabilities | Many sessions, each the same Devin | One session per issue, 59-minute cap | Multiple servers and conversations; no role separation | One session kind |
| Lifecycle | Thirteen columns: analysis review, code review, QA, PM UAT, human UAT, merge, deploy watch | None | Task in, PR out | Ticket to draft PR; take over in its IDE | Issue to PR | Conversation and task list | Work items and sessions in a workspace |
| QA | A separate QA agent that must execute: requests, browser, simulators | Whatever you ask it to run | Self-verification with screenshots and logs | Self-tests; your CI | Your CI on the PR | Whatever the agent runs | Not part of the product |
| After merge | Deploy recipes, health checks, incidents, rollback, store releases | None | None | None | None | Scriptable automations | Not part of the product |
| Usage limits | Each task parks with a resume time, other runs on that CLI are held, the task continues at reset (Claude Code sessions resume with --resume); unattended | Interactive session waits and continues at reset (esc to cancel); headless runs do not | Spend limit per agent at API rates | Plan limits | Premium request quota per plan | Your provider's limits | Beta |
| Data | Stays on the machine | Your machine; model calls to Anthropic | Code leaves your machine | Repos and secrets in Devin's environment | GitHub-hosted repos only | Wherever you host it | Your organisation's portal |
| Price | Free, Apache-2.0; your own CLI subscription or model API key | Claude subscription or API | Cursor plan plus API-rate usage | Individual and Teams plans | Paid Copilot plans | Free, MIT; cloud option | Beta; plans page |
| Runs under TaskTrooper? | — | Yes, one of its agent CLIs | Yes, the Cursor CLI | No | No; its PRs and CI are read | No | No |

Per-project write-ups, same content as [tasktrooper.ai/compare](https://tasktrooper.ai/compare):

- [TaskTrooper vs Claude Code](claude-code.md) — The terminal agent TaskTrooper runs underneath.
- [TaskTrooper vs Cursor Cloud Agents](cursor.md) — Parallel agents in cloud VMs that open merge-ready PRs.
- [TaskTrooper vs Devin](devin.md) — A cloud AI engineer you assign tickets to from Slack, Linear or Jira.
- [TaskTrooper vs GitHub Copilot coding agent](github-copilot.md) — Assign an issue to Copilot and get a PR from a GitHub Actions runner.
- [TaskTrooper vs OpenHands](openhands.md) — A self-hosted control center for coding agents and automations.
- [TaskTrooper vs Xirp](xirp.md) — An agent grounded in your organisation's services, docs and architecture decisions.
- [TaskTrooper vs Beads](beads.md) — A git-embedded, dependency-aware issue tracker and memory for coding agents.
- [TaskTrooper vs Beadhive and Gas City](beadhive.md) — A CLI software factory on top of Beads, with role agents and human gates.
- [TaskTrooper vs Vibe Kanban](vibe-kanban.md) — A kanban board where each task gets a branch, a terminal and a dev server.
- [TaskTrooper vs Claude Squad](claude-squad.md) — A terminal UI that runs many agent sessions in tmux and git worktrees.
