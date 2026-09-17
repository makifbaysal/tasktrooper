# Orchestration Agents

Skills and orchestrator rules are **agent-scoped**. Each skill and rule belongs to exactly one agent via `agent_id` foreign key. There is no global skills or rules catalog.

## Data model

- `skills.agent_id` → `agents.id` (NOT NULL, ON DELETE CASCADE)
- `orchestrator_rules.agent_id` → `agents.id` (NOT NULL, ON DELETE CASCADE)
- Unique constraint per agent: `(agent_id, name)` on both tables
- `agent_skills` junction table removed; `Agent.skill_ids` in API responses is derived from `ListSkillsByAgent`

## Default role agents

At boot `EnsureRoleAgents` creates any missing role agents (by name) and fills in missing skills/rules for partially seeded agents. The seed stores skills without embeddings, so it never waits on the embedder; `catalog.BackfillSkillEmbeddings` embeds them in the background (retried for up to 30 min). `/admin/agents` reports `seeding: true` until the boot steps finish (`bootseed.Booting`).

| Name | Subagent type | Effort | Purpose |
|------|---------------|--------|---------|
| `backend-developer` | `backend-engineer` | high | Go/Fiber/hexagonal and Java/Quarkus APIs, DB, tests |
| `frontend-developer` | `frontend-engineer` | high | React/Vite/Tailwind UI |
| `mobile-developer` | `mobile-dev-engineer` | high | Flutter, SwiftUI, Compose; store deploy |
| `product-manager` | `generalPurpose` | medium | Backlog, requirements, board tools |
| `qa-agent` | `generalPurpose` | medium | Manual test rounds, QA columns, read-only code tools |
| `system-architect` | `system-architect` | high | Analysis (`analiz`) tasks, code review, task decomposition |

Each role agent is seeded with 6–17 skills and 3–10 rules. Skill embeddings are filled in after the seed by the backfill above. Seed reconciliation updates existing skill/rule content on restart when the markdown under `internal/application/catalog/seeddata/` changes; renamed/removed entries must be listed in `deprecatedRoleSkills`/`deprecatedRoleRules` (`seed.go`) to be deleted from existing installs. Tool policies and effort are applied on agent CREATE only — admin customizations survive restarts, so policy additions reach existing installs via the admin UI.

### Seeded models

| Field | Value | Used for |
|-------|-------|----------|
| `provider_type` | `claude_code` | the CLI session a run is handed to |
| `model` | `sonnet` | every run and every subtask by default |
| `model_heavy` | `opus` | subtasks the planner rates `hard`, plus self-reflection and the golden judge |

Declared as one triple in `role_seed.go` (`roleAgentProvider` / `roleAgentModel` / `roleAgentModelHeavy`): a model name is only valid for the provider it was picked from, so the seed never writes a name without its provider. Both are aliases from `domain.ClaudeCodeModels()` rather than pinned ids, so a model release cannot stale them silently.

`fillRoleAgentModels` (`seed.go`) reaches existing installs on restart instead of a migration. It fills the pair only when **both** `model` and `model_heavy` are empty and `provider_type` is `claude_code` or still empty — either name set, or any other provider, and the agent is left untouched. The provider is stamped only where the `checkHostExecutor` probe says the CLI can run here; a host with no runner keeps empty models, because `CreateAgent` refuses that provider there and the seed would lose all six agents.

### QA test flow (seeded skill set)

**QA runs manual tests only.** The automation phase is deferred, not deleted: four skills (`e2e-automation-project`, `automation-pipeline-integration`, `test-doubles-wiremock`, `test-database-seeding`) are seeded through `mdSkillDisabled` and two rules (`e2e-automation-project`, `deterministic-test-env`) through `disabledRule`, so they are **disabled** — files and seed lines stay in place, but the prompt builder injects only enabled entries. `manual-only-testing` replaced the `manual-before-automation` rule (old name in `deprecatedRoleRules`). Seed reconciles the `Enabled` field too; before it did, a skill whose only change was that flag never reached existing installs. Re-enabling is `mdSkillDisabled` → `mdSkill`, no migration. The `mobile-manual-testing` skill defines which layer of a mobile task can actually run in this environment (build, the repo's own lint/tests, the API side of the flow) and how to report what cannot run **without approving it** — a Linux runner has no simulator.

`qa-agent` is designed for the two-phase flow below; phase 2 is currently off. It never tests in prod (`never-test-in-prod` rule):

1. **Manual verification** — scenarios come from the task definition + acceptance criteria, without reading code (`scenario-plan-first`); the environment is chosen (`test-environment-selection`: default is a local boot in the task workspace; if the repo maps a stage, `get_deploy_target` gives the stage `base_url`). Backend: boot API + worker, real requests, side-effect checks (`backend-manual-testing`, `worker-job-testing`). Frontend: drive the flow in headless Chromium, desktop/mobile screenshots, visual checklist (`frontend-manual-testing`, `ui-visual-evidence` rule).
2. **Automation** — after the manual pass, the same scenarios are added as tests to a separate automation project per repo (`qa-automation/`; api/worker/ui suites) (`e2e-automation-project`); external dependencies via WireMock, DB via Testcontainers (`test-doubles-wiremock`, `test-database-seeding`); the suite is wired into the repo's own pipeline (`automation-pipeline-integration`, repo `test_command`). A verdict needs three legs: manual evidence + tests added to the suite + a green pipeline (`qa-verify-before-verdict`).

**A QA run cannot finish without executing anything (`isUngroundedQA`).** The QA counterpart of the `analiz-read-code-first` gate, for the same reason: a QA run wrote its scenario list in the future tense ("I will apply these scenarios and verify each"), called nothing but a single board move, and was stamped `completed` — and the orchestration verifier, seeing a coherent plan, said PASSED.

- A run in `in_qa`/`ready_for_qa` cannot complete until at least one of `domain.QAExecutionTools` (`run_terminal` + `browser_*`) has run **successfully**; otherwise the run is marked `failed`, the rejection reason and the rejected text land on the task as a comment, and the reconciler dispatches a new QA attempt (bounded by `maxConsecutiveFailedRuns`=3).
- `review_criterion` / `add_task_comment` / `get_pipeline_status` **do not count as evidence**: the verdict is the claim under test and cannot be its own proof. Code-reading tools do not count either — QA is black-box, with three narrow, named exceptions (debugging an observed failure, scoping a resubmission's re-test from the diff, checking the case matrix is complete before execution) that feed the matrix or trace a defect but never substitute for execution; see `domain.RestrictCodeToolsForVerification` below.
- `analiz` tasks and runs that end in a question (`Clarification`) are exempt.
- The rule layer says the same: `qa-execute-in-this-run` (priority 100) + step 1 of `qa-agent.md`.

**The verifier no longer counts a plan as a result.** The section added to `buildVerifierSystemPrompt`: a result written in the future tense is a plan; when the goal is test/verify/review, executed evidence (the command run + observed output, a screenshot) **is the deliverable**, and its absence is a material gap. The board-delegation exemption ("a result that reads like analysis is not a failure / pass an unprovable run, the review chain will judge") does not apply here: for a QA run, **the review chain is that run**.

**The QA verdict gate holds every forward exit** (`criteriaReviewGate`). It used to gate only `ready_for_qa|in_qa → pm_uat` and `pm_uat → human_uat|done|released`; when QA moved a task straight from `in_qa` to `done`, no criterion verdict was required — that was the escape. Now every `isForwardReviewExit` target (pm_uat/human_uat/done/released) requires a full verdict, by the role of the review column the task leaves (QA or PM). `need_revision` and backward moves are ungated. Tasks with no criteria, and `require_criteria_complete=false`, remain no-ops.

Mobile automation is out of scope for now. Browser QA needs a Chromium binary at `CHROME_BIN`.

### PM UAT grounding + coverage-gap gate

PM had no equivalent of `isUngroundedQA`: a `pm_uat` run could approve every criterion with a tool ledger holding nothing but `list_task_comments`/`read_file`/`list_acceptance_criteria`, no `browser_*`/`mobile_*` call at all, and nothing refused that hand-off.

- **`isUngroundedPMUAT`** (`board/runner.go`) mirrors `isUngroundedQA`: a run in `pm_uat` with no `Clarification` and no successful call from `domain.PMUATExecutionTools` (the same browser/mobile names as `QAExecutionTools`, minus `run_terminal` — PM has no shell — and minus `get_deploy_target`, a lookup rather than an exercise of the product) fails the run (`ErrUngroundedPMUAT`).
- **`pmApprovedUncoveredCriterion`** closes a narrower hole: QA's `review_criterion(approved=true)` is a claim, not proof, when nothing in QA's own recorded round backs it. For every criterion this run's PM approved, the gate looks for a `domain.TaskTestCase` (read via `port.TaskTestCaseStore.ListByTask` / `list_test_cases`) with a matching `criterion_id` and `Status == passed`. If every approval this run made has one, the gate passes; if even one does not, the run also needs `PMUATExecutionTools` usage — approving an uncovered criterion off QA's note and board reads alone fails the run the same way `isUngroundedPMUAT` does.
- `pm-uat-review/SKILL.md` step 3 has PM call `list_test_cases` and check, criterion by criterion, for a `passed` case before approving off QA's note; step 5 requires PM's own `browser_*`/`mobile_*` walk-through for anything the case list does not already cover.

### Column-scoped code-tool restriction for verification (`RestrictCodeToolsForVerification`)

`RestrictToolsForVerdictColumn` only ever stripped writers/commit/merge/rollback; it never touched the read-only code tools, so QA and PM kept full code-reading access throughout `in_qa`/`ready_for_qa`/`pm_uat`/`human_uat` — enforced only by prompt wording (`qa-agent.md`, `product-manager.md`), not by anything the runtime checked.

`domain.RestrictCodeToolsForVerification(p, col)` applies on top of `RestrictToolsForVerdictColumn`, asymmetrically by role:

- **PM** loses every `CodeExplorationTools` name in `pm_uat`/`human_uat` — the only two columns PM signs a criterion off in. Outside those columns (backlog grooming, writing `technical_description`) PM keeps the set.
- **QA** loses only `read_file` in `in_qa`/`ready_for_qa`; `get_repo_tree`, `grep_code` and `get_task_pull_request` survive, because QA's three named exceptions above are tree-level and diff-level, never "read this file's full body and decide from it".

Wired at the same call-site pattern as `RestrictToolsForVerdictColumn`. An empty allowlist means unrestricted and stays unrestricted, same convention as the other two restriction functions.

Legacy agents (`general-coder`, `shell-runner`, `code-explorer`) are removed by migration `017_agent_scoped_catalog`.

## Admin API

Nested under agents:

| Method | Path |
|--------|------|
| GET/POST | `/admin/agents/:id/skills` |
| GET/PUT/DELETE | `/admin/agents/:id/skills/:skillId` |
| GET/POST | `/admin/agents/:id/rules` |
| GET/PUT/DELETE | `/admin/agents/:id/rules/:ruleId` |
| GET/POST | `/admin/agents/:id/tech-stacks` |
| PUT/DELETE | `/admin/agents/:id/tech-stacks/:stackId` |

Global `/admin/skills` and `/admin/orchestrator-rules` are removed.

## Tech stacks

`agent_tech_stacks` (migration 124) is one agent's list of technologies, and
`skills.tech_stack_id` files each skill under at most one of them. NULL is a
**general** skill — it holds whatever the code is written in; a set one only
applies inside that technology. Deleting a stack does not delete its skills:
the FK nulls them back to general. `POST/PUT .../skills` carry `tech_stack_id`;
a stack belonging to another agent is refused with 400
`tech stack belongs to another agent`. `UpdateSkillRequest` replaces the field
like every other, so omitting it files the skill as general.

## Agent templates

`agent_templates` (migration 030) stores read-only agent snapshots: agent fields + `skills`/`rules` JSONB. The role agents are seeded as built-in templates by `EnsureRoleAgents` (idempotent upsert by name).

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/admin/agent-templates` | List templates |
| POST | `/admin/agents?template_id=<uuid>` | Copy-on-create agent from template (body fields override name/prompt/model etc.) |
| POST | `/admin/agents/:id/template` | Save an existing agent as a template (upsert by name) |

Editing an agent created from a template never mutates the template. UI: AgentsPage offers "From template / From scratch" creation and a per-agent "Save as template" action.

## Planner and executor

- Planner loads per-agent skills and enabled rules into the system prompt
- Semantic skill search results include `agent_id`; planner validation rejects `skill_id` values that do not belong to the task's `agent_id`
- Executor injects **all enabled skills of the assigned agent** into every subtask's index (`enabledAgentSkills`), exactly like a board run. Disabled skills reach neither the index nor `load_skill`. The plan's `skill_ids` survive only as an emphasis line in the task prompt (`plannedSkillFocus`); ids naming a disabled or foreign skill are ignored, not fatal

### Subtask authority: `tool_names` only fences board writes

`tool_names` used to intersect the agent's whole policy through `IntersectToolPolicy`. A subtask declaring `[claim_board_task, move_board_task, add_task_comment]` therefore ran without being able to read the repo, and the agent asked the human "where are the files" — `clarificationGate` passes the question through in exactly that case (no read tool).

Now `domain.RestrictToPlannedTools`: the declaration decides **only** the board-write tools (`domain.BoardWriteTools`: `create_board_task` / `move_board_task` / `update_board_task`); every other tool configured on the agent (code reading, shell, web, `load_skill`) stays on every subtask. The duplicate-record guarantee holds, because an undeclared board-write tool is still withheld. `startedNotFinishedReason` still reads the plan's declaration as the intent signal, not the resolved policy — a "move DE-1 on the board" subtask is done once it moves.

### Subtask working directory: inside the checkout when there is one

Code tools and `run_terminal` resolve their root through `registry.EffectiveWorkspaceDir`; a set subtask workspace **wins**. A board run clones the repo under `task-<id>` and checks out the task branch before the agent starts, but the executor used to create an empty `task-<id>/<TASK_KEY>` folder **inside** that checkout and bind the tools there. Result: `get_repo_tree` returned nothing, the agent fell back to the real repo path from the board runner's prompt and guessed with `find <repo>/src`, and after 30 iterations reported "could not inspect the project structure, out of time". Two different `SubtaskWorkspaceNote`s (the runner's: repo root; the executor's: empty folder) contradicted each other in one prompt.

The clone itself was already right: `ensureWorkingCopy` clones from `Repository.RemoteURL` via `CloneRepo` (token auth) when no working copy exists, then `EnsureTaskWorkspace` checks out the task branch — a hard gate; if it fails the agent never starts. What was missing was not the clone but which directory the tools looked at.

`resolveSubtaskWorkspace`: if `workspace.IsRepoCheckout` (a `.git` exists) the subtask runs directly in the checkout; otherwise (chat orchestration over a bare workspace) the old per-subtask isolation is kept. Side benefit: files the agent writes now land in the clone itself, i.e. the tree the board runner commits.

### One request = one board record (duplicate guards)

There were several ways for a single chat request to open more than one board task; each is closed by its own gate:

| Path | Gate |
|---|---|
| Intake/planner did not see the record already opened in the conversation, so a "move" request was planned as "create" | `pipelineConversationHistory` preserves the session action ledger (the system message matching `domain.IsSessionActionDigest`); intake and planner prompts carry the rule "if this is one of those records, act on that id" |
| Planner split one job into "pick the location" → "pick the image" → "add", opening a task per step | `validateSingleTaskCreator`: at most one subtask in the **whole** plan may create a board task (`validateDisjointWrites` only looks inside one `parallel_group`, which `depends_on` chains slipped past). The prompt also says a subtask produces a CHANGE, not a decision or information: what only the stakeholder can know → `ready=false` + `questions`; what can be looked up in the repo/board/live site → the implementing subtask looks it up with its own tools |
| A subtask with empty `tool_names` was invisible to every check, yet the executor gave it the agent's whole policy | `subtaskMayCreateTasks` resolves empty `tool_names` against the agent's `ToolPolicy` (`domain.ToolAllowedByPolicy`). In the seed only `product-manager` has `create_board_task`, so developer/QA subtasks are unaffected |
| The verifier marked a correctly delegated run `passed:false` because "the feature is not live yet", and the replanner opened a second (analiz) task | `buildVerifierSystemPrompt` states that the run's deliverable is the **record**, and board latency (not live / code unchanged / QA not run) is not a finding |
| A repair plan was valid on its own, so it could add a second creator | `validatePlannerOutput` takes `priorTasks`; the replanner passes the original plan's subtasks, so creators are counted across the run |

Board tools' `task_id` parameter accepts a UUID **or** a board key (`DE-1`) (`ToolKit.resolveTaskRef`). A model sending the key used to get `invalid task_id`, conclude the task was unreachable, and open a new one.

### "Started" ≠ "finished" (subtask completion)

A subtask was stamped `completed` because the agent loop returned without error. A subtask whose own output was "I claimed the task and moved it to in_progress, now I will inspect the project structure" showed green `completed` in the UI.

`Executor.runTask` looks at the subtask's **own** tool ledger:

- Chat orchestration had no `ToolUsage` tracker (only `board/runner.go` set one up), so subtasks were unmeasurable. The executor creates one if missing; if present it leaves the board run's shared tracker alone and diffs before/after the loop via `registry.UsageDelta` (the analiz grounding gate's semantics are unchanged).
- `startedNotFinishedReason`: delta non-empty **and** entirely `domain.BoardProgressTools` (`claim_board_task`, `move_board_task`) **and** the subtask holds tools that could do other work → counted as unfinished.
- On first detection the error is fed back as `lastErr` (the existing "Previous attempt failed: …" mechanism) and the agent gets another attempt. When attempts run out the subtask is stamped `failed` **but the result is still returned** — the run does not blow up, the user sees the output, the label is honest.
- Two cases pass deliberately: a subtask that called no tool (conversational steps answered from context) and one that only holds claim/move ("move DE-1 on the board" — the move is the deliverable).

The planner prompt carries the matching rule: a subtask gets the tools its description requires. One holding only `claim_board_task` / `move_board_task` / `add_task_comment` cannot change code; it only updates the board.

### One run per task at a time

A task's record, and the agents working on it, multiplied from several places; each is closed:

| Path | Gate |
|---|---|
| A note the agent left on its own task via `add_task_comment` produced `task.commented`, which resolved to the assignee — the agent itself. `actor_agent_id` existed only in the move payload; the self-dispatch guard never saw comments | `actorAgentIDFromPayload` also reads a comment's `author_type=agent` + `author_id` as the actor |
| A comment left while the agent was running (the PM's kickoff note) queued a second run: `HasPendingForTask` looked only at `pending` rows, not `running`. That second dev run later started, claimed the task and **pulled it from code_review back to in_progress** | `HasLiveForTask` (`pending`+`running`) added; `Dispatcher.alreadyWorkingOn` uses it for comment events. Moves keep the old behaviour — a column change is real news, and the verify gate pushing a task back to in_progress is the only thing that plans the fix round |
| One create/update emits two events (`task.created`/`task.moved` + `task.assigned`). The first dispatched the agent; the second was guarded only by `HasPendingForTask` — when the worker flipped the first run to `running` between the two emits, the second event saw no queued run and opened a **duplicate**. The runner's one-run-per-task rule parked it and ran it as soon as the first finished: "the agent fires a second time right after a todo task moves to in_progress" | `alreadyWorkingOn` uses `HasLiveForTask` (`pending`+`running`) for `task.assigned` too. `actor_agent_id` was added to the `task.assigned` payload — it existed on move, not on assign, so an agent's own assignment was indistinguishable from someone else's |
| The runner could run two runs for the same task in parallel: the system-architect dispatched into code_review started without waiting for the developer run's **tail** (verification, fix rounds, commit, push) and reviewed a tree still being written | `Runner.beginTask`/`endTask`: one run per task. A job landing on a busy task is parked; when the task frees up the oldest parked job is re-queued. The worker is not blocked and moves on to other tasks |

### Column ownership: hand-off gates and terminal columns

`ready_for_qa` is the **queue** column (entering it triggers the pipeline + stage deploy); `in_qa` is where **testing happens**. Nothing in the flow moved into `in_qa`, so QA tested in `ready_for_qa` and jumped straight to `pm_uat`/`need_revision` — `in_qa` was a dead column and the board never showed "being tested now". Fixed in four layers:

| Layer | Change |
|---|---|
| `catalog/seeddata/agents/qa-agent.md` + `role_seed.go` | Step 0 of the QA flow: `ready_for_qa` → `in_qa` before testing. Rule `qa-enter-in-qa-before-testing` (priority 100); `qa-pass-to-pm-uat` / `qa-fail-to-need-revision` exit from `in_qa` |
| `board/runner.go` `columnInstruction` | Separate cases for `ready_for_qa` and `in_qa`. The general preamble says "moving to a column is not work, do not make it the plan's first item"; the `ready_for_qa` instruction keeps that and positions the move as **the opening act of the first test step** |
| `catalog/seed.go` + migrations 070 / 104 | `qa-agent` subscribes to `ready_for_qa`, `in_qa` **and** `done` (`done` = PR merge, not testing). Seed only writes subscriptions for agents that have none, so existing installs are backfilled by migration |
| `board/dispatcher.go` `isHandoffGateColumn` | `in_qa` + `human_uat` added |

In a column that is not a hand-off gate, `task.moved` resolves to the **assignee**. That woke the wrong agent in three places:

| Column | Old behaviour | New |
|---|---|---|
| `in_qa` | The moment QA started testing, the **developer** was dispatched — a second run started writing to the branch under test | Resolves to the column subscription (QA) |
| `human_uat` | A run was opened for the developer on a task waiting for human approval | Hand-off gate with no subscriber → no run at all (same pattern as `analiz_review`) |
| `done` / `released` | A run was opened for the finished task's assignee, and `columnInstruction`'s default told it "move to the next column" — `done` tasks slid to `released` without a deploy | `isDispatchSuspendedTask`: `released` always, `done` for every task type **except analiz**. For analiz, `done` is the human's approval; the architect opens the implementation tasks from there. The one exception is `doneMergeWake`, below |

The same logic applies to comments: a `task.commented` on a task in a hand-off gate column goes to the agent **holding** the task (QA, architect, PM). It used to go to the assignee: a comment on a task in `in_qa`/`code_review` woke the developer, and the agent actually doing the work never saw it.

### `done` = the column where the PR is merged (`doneMergeWake`)

`done` dispatched nobody, and the cost was invisible: the code of a board-approved task sat on its branch waiting for a human to merge it. Worse, task PRs were opened as **drafts** and nothing ever marked them ready — GitHub refuses to merge a draft PR, so those PRs were unmergeable by definition.

Two changes:

1. **Task PRs open ready-for-review** (`github.CreatePullRequest`, `git.EnsurePullRequest` — formerly `CreateDraftPR` / `EnsureDraftPR`). Draft had no counterpart in the flow: the PR opens the moment the work is handed to review. Nothing in code reads the draft state any more; `get_task_pull_request` still reports the `draft` field, and the merge tool un-drafts **old** draft PRs as a repair path (`MarkPullRequestReady`).
2. **A task landing in `done` wakes QA** — to merge it, via `merge_task_pull_request` (squash + branch delete). To keep the old bug out, four conditions must all hold (`board/dispatcher.go` `doneMergeWake`): column is `done`, the task type ships code (not analiz), the event is `task.moved`/`task.created` (not comment/assign/update — `UpdateTask` already emits `task.moved` only on a real column change), and the task has a recorded, **not yet merged** PR (`pr_url` set, `merge_commit_sha` empty).

The woken agent resolves from the **column subscription** (i.e. QA), never the assignee: `resolveAgents` returns the implementer for `done`, which is exactly the behaviour that produced the slide into `released`. One-shot is guaranteed by `merge_commit_sha` (migration 104) — the merge writes it, and every later event finds "nothing to do". Also: an agent is not dispatched on its own event, `alreadyWorkingOn` checks **running** runs on this path too (no two concurrent merges), and reconciler sweeps already skip `done`.

Three run-side adaptations: `producesADiff` (`board/review.go`) excludes `done` — a post-run commit after the merge would recreate the deleted branch on origin seconds later; `done` was added to `domain.verdictColumns`, so the merge run loses the file writers and `commit_task_changes` (`merge_task_pull_request` is exempt in this column and no other); and `columnInstruction`'s `done` case drops the default's "move to the next column" sentence (the next column is `released`).

The merge's own refusal matrix and what it checks: `.ai/tool-reference.md` → `merge_task_pull_request`.

### A board comment is something to act on

A card comment is written only when **someone has to do something**: a rejection with its reason, an error, a blocker, an unanswerable question, work not done. Everything that passes is silent — the column, the criteria, the PR field, the pipeline panel and the task history already say it.

| Removed comment | Where to look instead |
|---|---|
| the developer's green-run closing summary (`runner.go`, after the build gate) | run message + diff/PR |
| `merge_task_pull_request` success comment (`merge_pr.go`) | `board_tasks.merge_commit_sha` (in the PR block in the UI) |
| "Prod deploy succeeded — task released" (`pipeline.go` `moveTask`) | board history via `MoveReasonDeployReleased` |
| QA scenario plan, QA/PM "passed" comment, architect approve comment | `review_criterion` notes + the column |

The rule is written in three places: the `add_task_comment` tool description, `skills/shared/board-comment-style`, and `columnInstruction`'s column sentences. No comment repeats the PR link/number — it lives on the task's field and in the UI's right column.

### `done` / `released` = the columns where the deploy is watched (`deployWatchWake`, migration 105)

The merge put the code on the default branch; nobody asked what production did with it. "Merged" and "live and working" are two different facts that look the same on a card.

QA completes the same sequence in `done` — **merge → watch → roll back** — and also holds the `released` subscription (migration 105 backfill), because on a repo with a prod workflow the card moves to `released` on a green deploy, while on a push-to-deploy repo it still sits in `done`.

**The second carve-out is narrower than the first.** `deployWatchWake` does not look at the task's *state*, only at a payload key that only the sweeper (and the rollback dispatcher) writes: `domain.EventPayloadResumedResource == "deploy_watch"`. A state-based condition — "in done, merged, deploy unverified" — would recur on every comment and every update to the card and wake QA forever; the payload key can only be produced by the one caller that writes it. The column check still runs (`done`/`released`), and so does the type check (analiz ships no code).

**A pending deploy parks, it does not block.** `get_task_deploy_status` returns `domain.ResourceBlock{Resource: "deploy_watch"}` instead of a result; the loop ends its turn, the runner parks the card, and `board.DeploySweeper` (2 min) brings it back when GitHub reports the deploy finished. Same mechanism as the device park, with one structural difference: a device is a QUEUE (one phone; whoever frees it releases the next, oldest first), whereas two parked deploys are two separate things and either may finish first — so the sweeper lists the parked cards **without claiming**, asks about each, and claims only the ready one. Waiting costs one API call per park per pass and zero tokens; a retry loop on the agent side would spend one model turn per poll and hold a concurrency slot for the whole deploy.

**Run side:** `columnInstruction`'s `done` case describes the merge + watch sequence, and `released` has its own case (the default's "move to the next column" must never reach it — there is no column after `released`). `domain.RestrictToolsForVerdictColumn` removes `rollback_task_release` from every verdict column except `done`; `released` is not a verdict column, so it keeps it.

The tools themselves, their refusal matrices and the two rollback mechanisms: `.ai/tool-reference.md` → `get_task_deploy_status` / `get_deploy_logs` / `rollback_task_release`, and `.ai/architecture.md` → "Watching the deploy, and rolling it back".

### Undeployable is not `need_revision`

When an Actions dispatch is refused **because of the account** (402, spending limit, Actions disabled) the code is not at fault; `reportPipelineFailure` does not move the card in that case and only writes the reason on it (`githubapi.IsCIUnavailableText`, for deploy triggers). The decision belongs to the QA run in `done`:

| Situation | Action |
|---|---|
| the repo has its own deploy step (script / `make` target / README·`.ai` steps) | apply those steps via `run_terminal`, verify the environment answers |
| no deploy path (or the steps fail too) | the task goes to `blocked`, with one comment: merged, NOT deployed, reason |
| merge conflict (`dirty`/`behind`) | the task goes to `need_revision` — rebasing is the developer's job |

A merged-but-undeployed task is never left in `done`.

### `blocks` actually blocks (work_order park, migration 106)

`task_relations`' `blocks` type had existed since migration 022, and the only thing reading it was `repository.Service.validateMoveAllowed`: it refused a **move** into `todo`/`in_progress`. That only catches a human dragging the card. A task **created** directly in `todo`, the reconciler sweeping a never-started task, a sweeper handing a card back, an assignment event — all reached the dispatcher without passing through a move and started work that had been explicitly told to wait.

So the gate lives in the **dispatcher** (`board.WorkOrder`, inside `Dispatcher.Dispatch` right after the board event is written): the one place that starts runs. It runs only in the two columns where work BEGINS — `todo` and `in_progress`, the same pair `validateMoveAllowed` guards, so move refusal and dispatch park cannot disagree on "may this start". A task that reached `code_review` has its code written already; parking it there would leave a finished change behind a dependency that no longer exists.

**Park rather than refuse**, because a refusal is invisible: the card looks exactly like one nobody has picked up, and nothing says why. Parking says it — the `blocked` column, "waiting for T-1 (API migration) [in_progress] to finish" on the card, and a system comment naming every blocker.

The release is `board.WorkOrderSweeper` (1 min — the shortest of the four, because what it asks is one indexed query on the same database; the device sweeper asks the Appium hub, the deploy sweeper GitHub). Its shape is `DeploySweeper`'s, not `DeviceSweeper`'s: the work-order park is PER TASK — two parked tasks wait on two different blockers, and the last to park may be the first released. What it asks is the relation graph itself: `ListBlockingSources` returns the **unfinished** sources of a task's `blocks` rows, so an empty answer means "everything it waits on is done/released". The same query covers both ways a blocker disappears instead of finishing — a deleted task takes its `task_relations` with it via cascade (a cancelled blocker releases its dependants without anyone remembering), and a blocker moved back to `in_progress` shows up in the query again, so the dependant stays parked.

The sweeper's own hand-back carries `domain.EventPayloadResumedResource == "work_order"`, and `workOrderGateApplies` does not re-ask the question when it sees it — at best that would confirm what the sweep just answered; at worst a read racing the blocker's update would re-park the card.

An unreadable graph **stops** the dispatch (fail closed): a change written against a codebase whose prerequisite may not exist cannot be undone, whereas a skipped dispatch is picked up by the reconciler on its next sweep.

Direction: a `blocks` row stores the BLOCKER as `source_task_id` — the reverse arrow of `deploy_depends_on`, and it stays that way because flipping it would invert every stored row. No tool asks the agent to think in that direction: `create_board_task` / `update_board_task` take `blocked_by` ("this task waits for those"), exactly like `deploy_depends_on`. A cycle is refused where it is written, together with the chain that closes it.

### The analysis reference enters the run context (`derived_from`, migration 106)

Analysis (`analiz`) commits nothing to the repo: the architect's spec and plan exist only as `task_documents` attached to the analiz task. That had turned the implementation tasks opened after approval into work that could not reach its specification — a title, a description, and no path to the plan written for it.

`derived_from` is that path: source = implementation task, target = analiz task (same direction as `deploy_depends_on`; both are read from `task.Relations` the same way). It is not an ordering statement and gates nothing — `blocks` would make the analysis the gate for "when may implementation start" (a gate already passed: the human approved in `analiz_review`), and `deploy_depends_on` would refuse to release the implementation until an analiz task that ships no code reached production.

Run side: `Runner.analysisContext` (`runner.go`) resolves the relation through `repository.Service.AnalysisReferences`, writes the analiz task's documents into a system message, and places that block **right after the trigger message, before the diff and PR blocks** — those two say what was done to the task; this one says what the task should be. A run reading them in the reverse order reviews the change without knowing the specification it has to satisfy. It is given on every run, not just the first: a revision run fixes against the same spec, and the reviewer judging the diff judges against the same spec.

The block also names the tool that reads the documents — `list_task_documents` (the read half of `add_task_document`, as `list_task_comments` is to `add_task_comment`). Without it, the same model that said "let me read the comments" and wrote a comment would try to read a document and write one. Migration 106 backfills it to every agent that has `list_task_comments`.

### A role agent's code-reading tools arrive independently of its policy

`UpliftWorkspaceTools` treated any agent with `_board_` in its allowlist as "role-scoped" and skipped all code tools. A role agent's `ToolPolicy` is written **only at agent CREATE** (so admin customizations survive restarts), so when a role gained code tools later, the row on an existing install stayed without them. Result: the `system-architect` dispatched into code_review ran without a single tool to read the diff it was to review, and spent its run writing "I do not have the required tools" on the task.

`domain.CodeExplorationTools` (`codebase_search`, `grep_code`, `get_repo_tree`, `get_symbol_skeleton`, `expand_symbol_context`) is given to every tool-scoped agent like `workspaceReadAlwaysTools` — all read-only, none widens what the agent **can change**. `run_terminal` and `mcp_filesystem_*` remain subject to the operator's policy for role-scoped agents.

### `code_review` = reading the PR, not running it

A run in the review column passes judgment on someone else's diff; it is not the run that builds and fixes it. `columnInstruction`'s default used to tell the architect "do the work your role wants in this column", which the architect read as "clone, compile, run the tests" — reproducing the pipeline that already ran on entry.

| Layer | Change |
|---|---|
| `board/runner.go` `columnInstruction` | `code_review` case: **read** the PR diff; do not run the app/build/tests, do not fix it yourself. Three axes: (1) is the requested work done (AC), (2) is the code itself sound, (3) what else in the domain does the change break — for the third, surrounding code the diff touches may be read freely |
| `board/review.go` `isReviewColumn` | `code_review` / `analiz_review` / `pm_uat` runs skip the build gate (`verifyAndFix`) and commit/push. When a review run entered a fix round, the architect became the implementer and approved its own patch at the same gate; a verify failure pushed the task back to `in_progress` in the reviewer's name |
| `board/review.go` `reviewDiffMessage` | The review run's diff budget is 24 KB instead of 8 KB. "Read the whole diff" was unfulfillable at an 8 KB cut |
| `catalog/seeddata/agents/system-architect.md` + `code-review-rubric.md` + `role_seed.go` | Prompt, skill and rule say the same: review is reading; the "Domain impact" heading requires reading callers outside the diff and naming the affected `file:line`. Rule: `code-review-reads-never-runs` |

### No code review without a PR

Review goes through the PR, but the path that opened it was async and best-effort: the developer's branch is pushed at the **end** of the run, and the PR attempt triggered by the column transition lost the race against that push, leaving no PR at all.

- `git.PushBranch` (port method): publishes the branch without committing. Deliberately different from `CommitAndPush`: the tree in the review workspace belongs to the developer under review, and `git add -A` would add their leftovers to the branch.
- `board/review.go` `ensureReviewPR`: guarantees the PR before a `code_review` run starts — if `EnsurePullRequest` fails, push the branch and try once more. With no PR the run fails with `ErrReviewPRMissing` and writes the reason on the task (the task stays in `code_review`; the reconciler retries). A repo without an origin cannot have a PR; there the review is done over the diff.
- `board/pipeline.go` `resolveGitInfo` does the same push-then-retry: the pipeline runs on column entry, so that is the earliest the PR opens.
- `adapter/git` `EnsureTaskWorkspace`: after fetch, if `origin/<branch>` exists, `checkout -B branch origin/<branch>`. Before, a fresh workspace (a first checkout of the task, or one whose local directory had been deleted and recreated) cut the branch from default and saw an **empty diff** — the run meant to review the work sitting on origin got nothing.

### The phase after the agent goes quiet is visible

A board run does not end with the agent's last message: build/vet verification, N fix rounds, commit and push follow. That phase wrote no steps, so the newest entry in the activity panel was the agent's last tool call, and a completed plan looked stuck under a frozen `run_terminal running…` label. The runner writes `build_verification_start` / `build_verification_passed` / `build_verification_failed` steps.

## UI routes

```
/orchestration/agents
/orchestration/agents/:agentId/settings
/orchestration/agents/:agentId/skills
/orchestration/agents/:agentId/rules
```

Legacy `/orchestration/skills` and `/orchestration/rules` redirect to the agents list.

## Self-evolution, memory, KPIs (migrations 031–035)

- `agents.self_evolution_enabled` (031): gates whether the reflection engine may apply skill/rule changes for the agent. Toggle in AgentSettingsPage. Templates carry the flag + `kpis` JSONB (035).
- **Memory** (`agent_memories`, 032 + 065): memories with JSONB embeddings, in four buckets from two nullable columns — `agent_id NULL` = team memory, `repository_id NULL` = global (see [Memory scopes](#memory-scopes-migration-065)). Inline tools for all role agents: `save_memory`, `search_memory`, `delete_memory` (`internal/adapter/tools/memory`). Injected into board runs and agent chats via `memory.Recall` + `prompt.MemoryContextMessage` (8 project + 8 global, rendered as separate sections). Service: `internal/application/memory`.
- **Lazy skills**: `BuildSystemPrompt` injects a skill **index** (name + description) instead of full content; agents fetch full instructions at use time via the `load_skill` tool (`internal/adapter/tools/skill`).
- **KPIs** (`agent_kpis` + `agent_kpi_results`, 035): definitions live on the agent, measured per agent per period (daily/weekly ISO/monthly). Only metrics in the Go registry (`internal/application/kpi/registry.go`) are accepted: `tasks_completed`, `revisions_received`, `uat_failures`, `failed_runs`, `bugs_assigned`, `first_pass_rate`, plus the column-time metrics below. Attainment: full target → 1.0, half target → 0.5, else 0. Composite = weighted mean × 100. KPI context is injected into agent prompts ("your objective: meet these KPIs"). Admin CRUD: `/admin/agents/:id/kpis`; registry list: `GET /v1/kpi-metrics`. Role agents get default KPIs on seed (`defaultRoleKPIs`).

### Evolution engine (migration 033)

- **Triggers and evidence** (`agent_reflections` + `agent_evolution_events`; `internal/application/evolution`): triggers = periodic ticker (`reflect_interval`), task moved to `need_revision` (debounced), manual `POST /v1/agents/:agentId/reflect`. Incremental: each reflection covers only the window since the previous one and compares against the prior reflection's `performance_snapshot` baseline. Evidence = window chat messages, task runs, revision comments, score events, KPI attainment, current skills/rules/memories, and a regression report. Output = strict JSON (skills/rules/memories/reverts + self-assessment); skills/rules are applied only when `self_evolution_enabled`, with before/after snapshots recorded per change. With `evolution.allow_web_research` the reflection runs through agent.Loop with `{web_search, fetch_url}`.
- **Golden gate (auto-revert)**: with `evolution.golden_gate` on, when a reflection proposes skill/rule changes the golden suite runs **before** and **after** the change. The decision is made not by the model that wrote the reflection but by an independent judge LLM (`evolution.judge_model` / `judge_provider_type`; the reflection model when empty): it gets the before/after pass rate, which golden task missed what, and the list of applied changes, and returns `{"keep":bool,"reason":string}`. If the rate dropped, revert without consulting the judge; if the judge is unreachable, fall back to "keep unless regressed". Revert is **all-or-nothing**: every skill/rule change in the set is undone in reverse order, a `change_type=revert` event is written for each, and the original event is marked `impact=regressed` without waiting for the impact window. The outcome lands in the reflection summary as a `Golden gate: %X → %Y | verdict ...` line; `performance_snapshot.golden_pass_rate_before` is stored too.
- **Skill/rule budget**: `evolution.max_skills_per_agent` (25) and `max_rules_per_agent` (15) are per-agent totals. The reflection prompt carries a "merge/update first" instruction plus used/total budget; on the apply side a `create` at budget is rejected (a "rejected" line in the applied log), and a `create` with an existing name is converted into an `update` of that skill. The agent's own `create_skill` tool applies the same cap.
- **Versioning / restore**: **every** write to a skill or rule is appended to `catalog_versions` (095) — create/update/delete/restore, with its source (`user` | `evolution` | `seed`; for evolution, the `reflection_id`). Deleted content is written too, so it can be brought back. API: `GET /admin/agents/:id/skills/:skillId/versions`, `POST .../skills/:skillId/restore` `{"version":N}` (same for rules under `rules/:ruleId`). A restore produces a new version; history is never rewritten.
- **Impact tracking**: pending events older than `impact_window` are classified `effective` / `regressed` / `neutral` / `insufficient_data` by comparing score events before vs after the change. Regressed unreverted changes are surfaced to the agent's next reflection; the agent decides to revert (before-snapshot restored, `change_type=revert`). Impact alone never auto-reverts — the golden gate above is the only automatic revert.

### Memory scopes (migration 065)

Two nullable columns on `agent_memories` produce four buckets:

| | `repository_id NULL` (global) | `repository_id` set (project) |
|---|---|---|
| `agent_id` set | `agent_global` — the agent's habits and preferences, valid anywhere | `agent_project` — what the agent learned inside that repository |
| `agent_id NULL` | `team_global` — workspace conventions every agent reads | `team_project` — that repository's shared facts (build command, deploy flow) |

- **Writing**: `save_memory` takes `scope` (`project` \| `global`) and `shared` (team vs personal). Unset `scope` means "wherever I am" — project-scoped when a repository is in context, global otherwise. `scope=project` with no repository in context is an error, not a silent global write.
- **What is refused** (`domain.MemoryRunLogReason`, applied by `save_memory` and by the reflection job): content anchored to one card — a task key, `PR #n`, a commit SHA, a column move (`code_review→ready_for_qa`), "this task/run". Run narration belongs in the task's comments; the tool answers with the reason and the durable rewrite. A save whose wording already matches a memory in the same bucket (`domain.MemoryDuplicateOf`, Jaccard ≥ 0.6) returns `saved:false` with `duplicate_of` instead of adding a second copy.
- **Reading**: `search_memory` defaults to the visible set (this repository + global, own + team) and accepts `scope` (`all`\|`project`\|`global`) and `owner` (`all`\|`self`\|`team`). Query building lives in `buildMemoryListQuery` (`adapter/store/postgres/memory.go`) and is unit-tested there.
- **Repository in context**: board runs get it from `job.RepositoryID`, chats from `session.ProjectID` (the column is `sessions.repository_id`; the Go field kept its old name). With no repository, project memories are unreachable — another repo's lessons never leak into a run.
- **Eviction**: `memory.max_count` applies per (agent, repository) bucket, so a busy repository cannot evict what the agent learned elsewhere. Team memories are exempt.
- **Reflection** writes global memories only: it reasons over runs from every repository at once and has no single project to bind a lesson to.
- **API**: agent and shared memory listings accept `repository_id` + `repo_scope` (`any` default \| `project` \| `global` \| `visible`); create bodies accept `repository_id`. Scope is fixed at creation — updates never move a memory between buckets.
- **UI**: `MemoryManager` has a scope filter (all / global / per repository), a scope select in the create form (locked while editing), and a scope badge per row.

### Column-time KPIs and defect attribution (migration 057)

`task_column_spans` records one row per uninterrupted stay of a task in a column, with the agent that worked it. Spans are written from `Dispatcher.Dispatch` — the only place a board event is created — and the agent is claimed by the first run created in that span. A rework loop produces a second span for the same column with a higher `visit_no`.

Time metrics (all lower-better, median hours, weekly by default):

| Key | Columns | Seeded target (full / half) |
|---|---|---|
| `clean_time_in_progress` | `in_progress` | dev 6h / 16h · architect analiz 4h / 12h |
| `clean_time_code_review` | `code_review` | 1h / 4h |
| `clean_time_in_qa` | `ready_for_qa` + `in_qa` | 3h / 8h |
| `clean_time_pm_uat` | `pm_uat` | 2h / 6h |
| `review_escapes` | — (score events) | 0 / 1, weight 2 |
| `tool_error_rate` | — (`task_agent_runs.tool_calls/tool_errors`, migration 071) | lower-better, %, min sample 20 calls |

Two rules keep speed from being bought with quality:

- **Clean-only sample.** Only tasks that reached `done`/`released` without ever entering `need_revision` are measured (`board_tasks.clean_completion`, stamped by `CompletionStamper`). A rushed task that bounced leaves the sample entirely — hurrying removes the reward rather than increasing it.
- **Minimum sample of 3.** Below that the resolver returns `ErrInsufficientData` and no result row is written, so `CompositeScore` drops the KPI from the weight sum. Writing a zero would score 1.0 on a lower-better metric and make idleness look like maximum speed.

Time in `blocked`, `human_uat`, `analiz_review`, `backlog` and `todo` is never charged to an agent: those are human latency or nobody's work.

Penalties are charged to **span owners**, not to `task.assignee_agent_id`:

| Rejection | Charged | Event (delta) |
|---|---|---|
| `code_review` / `ready_for_qa` / `in_qa` → `need_revision` | dev | `revision_requested` (−10) |
| `pm_uat` → `need_revision` | dev + QA | `pm_uat_failed` (−5 each) |
| `human_uat` → `need_revision` | dev + QA + PM | `human_uat_failed` (−8 each) |
| Human rejects in `code_review` where the reviewer's verdict was `approve` | architect | `review_escape` (−10) |

The later a defect is caught, the more it costs. An agent that held two of the charged stages is charged once.

### Review mode: `repositories.require_human_review` (migration 047, semantics changed in 057)

The flag used to mean **human instead of agent**: with it on, the architect and PM were never dispatched into `code_review`/`pm_uat`. It now means **human after agent**:

- The reviewing agent always runs. When it tries to advance the task, `board.ReviewGate` converts the move into a recorded verdict (`review_verdict = approve` on the open span), leaves the task in place, and waits for the human.
- When the reviewing agent moves the task to `need_revision`, the verdict is `reject` and the move **proceeds** — human approval gates letting work through, not sending it back.
- When the human then rejects a span whose verdict is `approve`, the reviewer is charged `review_escape`.

Moves carry an explicit actor (`UpdateBoardTaskRequest.Actor`, never parsed from the request body): `agent` from the board tools, `human` from the HTTP API, and the zero value `system` from the pipeline and verification steps — so an automated move to `need_revision` after a red build is not mistaken for a human rejection.

**Cost note:** repositories with the flag on spend tokens on the architect and PM, which they did not before.

### Lifecycle gates: `repositories.require_review_chain` / `require_release_deploy` (migration 080)

Two more per-repository flags, both `BOOLEAN NOT NULL DEFAULT false`, independent of `require_human_review` and of each other. `PUT /v1/repositories/:id/lifecycle-gates` arms/disarms either or both (an omitted field is left unchanged); see [api-spec.md](api-spec.md). Code: `internal/domain/lifecycle_gate.go`, `internal/application/repository/lifecyclegate.go`.

- **`require_review_chain`** blocks a move into `done` — and into `released` when that would skip `done`, but not the ordinary `done → released` promotion, since `done` already asserted the chain — unless the task has actually visited every stage `domain.ReviewChainForType` lists for its type: `code_review` → `in_qa` → `pm_uat` for `task`/`bug`, `analiz_review` alone for `analiz`. Evidence is whether the task's span history (`task_column_spans`, migration 057) ever contains that column, **not** the current column and **not** a verdict field — so rework through `need_revision` and back is never punished, it just re-earns the stage. Where a verdict *was* recorded (only while `require_human_review` is on), a stage whose most recent visit ended in a recorded `reject` blocks too, even if the column was visited. A stage whose column is missing from this board's `board_columns` is skipped rather than counted as failed — a customized board cannot route a task through a column it does not have.
- **`require_release_deploy`** blocks a move into `released` unless `task_pipelines` (migration 040) has a `prod_deploy` run with `status = success` for the task, or a `preprod_deploy` success on a repository with no prod workflow mapped (the same fallback `PipelineRunner` itself uses to call preprod the release). `status = skipped` (migration 067: no workflow mapped, nothing ran) is never accepted as evidence. `analiz` tasks are exempt (`domain.TaskTypeShipsCode`): they ship no code, and their own workflow drives `done → released` once the implementation tasks they produced exist.
- **Both are opt-in and default off** for the same reason `require_criteria_complete` and `require_human_review` are: each is only honest on a board actually wired for it. A repository with no QA agent subscribed to `in_qa`, or a customized board missing that column, could never satisfy `require_review_chain`. A repository with no prod deploy workflow mapped always records its prod deploy as `skipped`, which `require_release_deploy` never accepts — turning it on there parks every task in `done` forever, since nothing can ever earn `released`. Enabling either flag is the repository owner asserting their board can actually clear it.
- **Both fail closed** on evidence they cannot read (span store or pipeline store unavailable, or a lookup error) — the same direction every other board gate in this file fails, since a check that passes when its input is missing is not a check. The block error names the missing/rejected stage (or the missing deploy) and the remedy move, the same way `ErrMigrationNotStaged` does for the migration gate.

### The system moves a task from the queue column to the working column (`Runner.enterWorkingColumn`)

Entering the column was left entirely to the agent's own `move_board_task` call. Until the model got to that tool the board said `todo` — and when it never got there, forever: DE-1 sat in `todo` while a `frontend-developer` run was working on it. The system moves it as the run starts, before the prompt is built.

- **Only the assignee's own run.** A `todo` task fans out to every agent the column resolves to, and each is told "do not touch what is not yours"; claiming on their behalf would hand the task to whichever agent the dispatcher reached first. **An unassigned task still waits for the agent's claim.**
- **The move is attributed to the agent** (`Actor=agent`, `ActorAgentID`), so the dispatcher's `actorAgentIDFromPayload` guard does not open a second run for this event — it is indistinguishable from the move the agent would have made.
- If the board write fails the run continues; the agent's own move still corrects the column.
- `job.Task` carries the updated state, so `columnInstruction` renders the `in_progress` branch and the planner does not spend its first step on a move that already happened.

`ready_for_qa → in_qa` lives in the same function. Left to the agent it produced the same failure: the QA run wrote "first I will move to `in_qa`", finished `completed` without testing, and the card stayed in `ready_for_qa` — and since the review-chain evidence is the `in_qa` span, that task could never reach `done`.

- **Who moves differs because the queue dispatches differently.** `todo` fans out to the assignee (rule above); `ready_for_qa` is a hand-off gate column (`isHandoffGateColumn`): the dispatcher resolves it by **column subscription** while the assignee is still the developer. So every run landing there is already that column's QA agent, and no assignee check is needed.
- `analiz` tasks are excepted — they have no QA stage.
- The move is again attributed to the agent, so **no second QA run opens**: the run continues from where it started with the `in_qa` instruction, and testing happens in the same run.
- If the automatic move is refused, `columnInstruction`'s `ready_for_qa` branch remains and asks the agent to make the move itself; in the normal flow the agent reads the `in_qa` branch.
- Step 0 of `qa-agent.md` is written accordingly: "you are already in `in_qa`; do not plan a step for that move, do not wait for a second run".

## Agent loop guards (`internal/application/agent`)

A run stops making progress in three distinct ways; each has its own guard. All live in `runToolCalls`, on `callTracker`.

| Guard | Trigger | Effect |
|---|---|---|
| **Repeat** | Same tool + same args + same result | On the 2nd repeat, `repeatNudgeMessage` replaces the result; at 4 the run is `stuck` |
| **Skip** | The same call **immediately after** the above | The tool is **not executed**; the nudge is returned (`RunStats.SkippedRepeats`) |
| **Error streak** | 3 consecutive failing tool calls (args may differ) | `errorStreakMessage` is prepended to the result; at 8 the run is `deadEnd` |
| **Per-tool errors** | One tool failed 5 times over the run | `toolErrorMessage` is appended to the result |

- The repeat guard only matches a call **against itself**; the same `sed` error in five different files is invisible to it — that is what the error streak is for.
- The skip rule is kept narrow: the call must be **identical** to the previous one and its result must already have come back the same twice. If another call intervened, it is re-executed, because that call may have changed the answer.
- Every successful call resets the error streak; failing repeatedly while fixing a build is normal, with successes in between.

### The context budget is applied inside the loop too

The `context.Budget` given via `Loop.SetHistoryBudget` is applied **on every iteration** (and on the wrap-up turn). It used to be applied once before entering the loop; the loop's own tool results pushed the request many times over budget again (80 iterations × ≤`max_tool_output_chars`). The result was the provider dropping the connection — what looked like a network error was a request nobody could serve.

### Provider error classification

`domain.LLMHTTPError` separates three classes and `chatWithRetry` applies the one treatment that works for each:

| Class | Example | Behaviour |
|---|---|---|
| `retryStop` | 400, 401, 403, 404, 422 | Never retried |
| `retryBackoff` | 408, 409, 429, 5xx, transport error | 500ms → 1s → 2s (cap 8s), context-aware |
| `retryShrink` | 413, or 400/422 + "context length" / "prompt is too long" | The conversation is **halved** and resent |

The shrunk conversation is returned to the caller (`chatWithRetry` returns the messages too); otherwise the next iteration would rebuild the rejected request.

### Error information outlives a run

- **Across attempts** (`orchestrator.priorAttempt`): the previous attempt's tool counts, last calls and which tools kept failing enter the next attempt's prompt. Only the error text used to carry over; since messages were rebuilt from scratch, every attempt repeated the same discovery.
- **Across runs** (migration 071): `task_agent_runs.tool_calls / tool_errors / error_pattern`. The next run on the same task reads the previous failed run's `error_pattern` as a system message. The `tool_error_rate` KPI is computed from the same columns.
- `registry.ToolUsage` counts errors too (`RecordError`/`Failures`/`Totals`), but **in a separate map**: gates built on `Count`/`UsedAny` ask "did the agent actually do this", and a failed call is not evidence of that.

## Quality gates and new tools (migrations 036–037)

- **Verification gate** (`board.verification_enabled`): when a board run ends, `go build ./...` + `go vet` (+ the package.json build script) run in the task workspace; error output is fed back to the agent (`verify_max_fix_attempts` rounds). A persistent failure → the task returns to `in_progress` + a system comment (`internal/application/board/verify.go`).
- **AC gate** (`board.require_criteria_complete`): a task with criteria cannot move to ready_for_qa/done/released until all criteria are complete. Board tools: `list_acceptance_criteria`, `set_criterion_completed`.
- **Per-role AC verdicts** (`task_criterion_checks`, migration 073): the implementer's `completed` mark is a claim; QA (`ready_for_qa`/`in_qa`) and PM (`pm_uat`) record their own approve/reject per criterion via `review_criterion` — the role derives from the task's current column, not the agent's say-so, and a reject note is mandatory. `criteriaReviewGate` (same `require_criteria_complete` flag): in_qa→pm_uat needs QA approval on every criterion, pm_uat→human_uat/done needs PM approval; moves to need_revision are never gated. Verdicts reset when the task re-enters `ready_for_qa` (except in_qa→ready_for_qa — same round). The drawer shows the QA/PM badge and reject note per criterion; existing installs get the tool policy backfilled by migration.
- **Criterion cancellation** (`task_acceptance_criteria.canceled/cancel_reason`, migration 131): the criterion's third state. `cancel_criterion` (reason mandatory) takes the criterion out of scope and writes the reason as a task comment too; gates count a cancelled criterion as "settled" (never "met") and expect no QA/PM verdict. The tool policy is added at boot via `catalog.grantMissingRoleTools` to every role that has `set_criterion_completed` — a migration does not backfill it (see `role_tools.go`).
- **Run-end loop** (`board/criteria_sweep.go`): before a run closes, unticked criteria are handed back to the agent in the same conversation, for at most `criteriaSweepRounds` (3) rounds. The answer is one of three: tick, cancel with a reason, or **do the work now** and tick. After round 1 the prompt hardens ("no summary, make the change"). When the round budget runs out the card parks with open criteria and a system comment says why.
- **Test cases** (`task_test_cases`, migration 130): QA's round lives on the card — a separate area next to the acceptance criteria. Each case: `title`, `category`, `status` (planned/passed/failed/skipped/invalid), `expected`/`actual`, `evidence`, `notes`, optional `criterion_id` (empty = a case derived from the request, not written by a criterion). Tools: `record_test_cases`, `set_test_case_result`, `list_test_cases`. `testCaseGate`: a forward exit from `ready_for_qa`/`in_qa` is refused if there are no cases or any case is still `planned`; `failed` does not block (its exit is need_revision). The drawer shows them under the criteria.
- **Task branch diff**: every board run with a workspace gets the branch diff (against base, 8K chars) injected into its prompt — revision and QA runs see the changed code directly (`git.TaskDiff`).
- **Team-shared memory**: `agent_memories.agent_id NULL` = team memory; `shared: true` on the `save_memory` tool. Shown with a `[team]` tag in the prompt; shared entries are exempt from eviction.
- **Golden eval** (`agent_golden_tasks` + `agent_golden_results`, 037): after every reflection that applies a skill/rule change, the agent's golden tasks are replayed tool-less with the current skill/rule set (expected-substring check). The pass rate is written to the reflection summary + `performance_snapshot.golden_pass_rate`. CRUD: `GET/POST /admin/agents/:id/golden-tasks`, `PUT/DELETE .../golden-tasks/:goldenId`, `GET .../golden-results`.
- **Retrieval**: chunk search is pgvector HNSW (when available) + pg_trgm trigram RRF hybrid; the injector does multi-query via `indexer.query_rewrite`; skeleton injection follows symbol-graph fan-in order (most-referenced files first).

## QA pipeline (migration 040)

- **task_pipelines + task_pipeline_jobs**: when a task moves to `ready_for_qa`, `board.PipelineRunner` triggers the build/test pipeline asynchronously in the background (worker pool) (`Trigger`, `PipelineTriggerReadyForQA`); a pending pipeline for the same task is superseded by the new one. `POST .../pipelines` re-triggers manually (`PipelineTriggerManual`).
- **Stage selection** (`commands.go`): the repo's `verify_command`/`build_command`/`test_command` are used when set; otherwise multi-language auto-detect by marker file — go (`go.mod`), node (`package.json` npm script), rust (`Cargo.toml`), python (`pyproject.toml`/`requirements.txt`), maven (`pom.xml`), gradle (`build.gradle[.kts]`). With a `Dockerfile`/`Containerfile` at the repo root and a resolvable container runtime, the build stage becomes a container build (`podman build .` / `docker build .`); the runtime is the admin setting `pipeline_container_runtime` (`podman`/`docker`/`auto` — auto tries podman first on PATH).
- **QA gate**: `pipelineGate` in `dispatcher.go` defers the `task.moved` event that would normally fan out to QA agents immediately on entering `ready_for_qa` until the pipeline result (the event is still recorded; board history stays complete). On success, `PipelineRunner` → `DispatchQA` (`SkipPipelineGate: true`) creates the QA agent runs. On failure the task returns to `need_revision` + the failed stage's tail-truncated log (4000 per stage, 3000 total chars) as a system comment.
- **No checks defined (migration 067)**: with no validate/build/test job mapping on the repo, `finishNoChecks` runs — provider `none`, a single `skipped` job, pipeline status `skipped`. NOT `success`: it still opens the gate (`domain.PipelineStatusOpensGate` → `DispatchQA` runs) but shows no green badge in the UI, because nothing was compiled or tested. It used to write `success` and the UI showed a "Success + Did not run" contradiction.
- **Agent visibility**: the `get_pipeline_status` board tool (repo-scoped; returns the task's latest pipeline + job results, with tail-truncated logs for failed jobs), and for tasks landing in `need_revision` — when the latest pipeline is `failed` — an automatic "Pipeline failure" report is injected into the run prompt (`runner.go`).
- **API**: `GET/POST /v1/repositories/:id/tasks/:taskId/pipelines`, `GET .../pipelines/:pipelineId`; the `BoardTask.latest_pipeline_status` field is injected into task-list endpoints with one batch query (no N+1) and shown as an icon on the board card.
- **Repo deletion**: from the `RepositoriesPage` card menu or the repo settings "Danger zone"; every FK onto `repositories`/`board_tasks`, including `task_pipelines`/`task_pipeline_jobs`, is `ON DELETE CASCADE` — the cascade audit found no gaps.
- **No-task-workspace fallback**: `execute` tries `WorkspaceRoot/task-<id>` first, then the repo root (`ResolveRootPath`); if neither resolves (e.g. the repo was deleted while the pipeline waited in the queue) the pipeline closes as `failed`/`interrupted` with no side effects — no command runs, no QA is triggered, the task is not moved.
