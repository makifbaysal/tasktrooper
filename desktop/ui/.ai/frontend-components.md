# Frontend Components (Atomic Design)

The web UI (`src`) follows **Atomic Design**. Every piece of UI is one of: **atom → molecule → organism → template → page**. This is a hard rule for all UI work.

## The Rule (read before writing any UI)

1. **Reuse first.** Before writing markup, find the existing atom/molecule/organism that fits. Never hand-roll a button, card, badge, input, dialog, header, empty state, list row, etc. — import the shared component.
2. **No ad-hoc equivalents.** If you catch yourself writing `<div className="rounded-lg border p-4">` that duplicates `Card`, or a bare `<button className="...">` that duplicates `Button`, stop and use the component. Raw elements are only for genuinely one-off layout wrappers (`div`, `section`) with no shared equivalent.
3. **New shared component?** If a pattern repeats 2+ times and no component covers it, create one at the correct atomic level (see placement below) and use it everywhere the pattern appears.
4. **Updating a shared component is allowed, but must not break callers.** See "Safe updates" below.
5. Every new page composes molecules/organisms inside a template (layout); it does not re-implement shells, sidebars, or headers.

## Levels & Placement

| Level | What it is | Lives in | Examples |
|-------|-----------|----------|----------|
| **Atom** | Single-purpose primitive, no business logic, style-only | `components/ui/` | `button`, `card`, `badge`, `input`, `label`, `checkbox`, `switch`, `select`, `textarea`, `dialog`, `separator`, `skeleton`, `spinner`, `elapsed` (a live elapsed-time leaf: every 1s in the first minute, then every 30s), `progress`, `scroll-area`, `empty-state`, `stat-tile` (label/value/foot KPI tile, `tone="warning"`) |
| **Molecule** | Small composition of atoms, reusable, little/no state | `components/admin/`, `components/layout/`, `components/markdown/`, `components/attachments/`, feature dirs | `PageHeader`, `FormDialog`, `KeyValueEditor`, `MultiSelectPicker`, `ToolPolicyForm`, `PageContent`, `SidebarNavLink`, `layout/PageSuspense` (the spinner `<Suspense>` every layout puts around its `<Outlet />` — pages are lazy chunks, see `App.tsx`), `layout/LeadQuickAsk` (header "Ask the PM" field), `agent/AgentAvatar` (initials avatar; `lead` variant marks the lead agent), `activity/AgentRunHeader` (agent + status + elapsed + live "now" line + stat facts), `activity/RawStepList`, `activity/FeedStatusIcon`, `activity/ToolKindIcon`, `HealthStatus`, `MarkdownContent`, `MarkdownField`, `ActivityFeedItem`, `WizardStepper`, `TypingIndicator`, `chat/MentionTextarea` (textarea with the @-mention menu; every message box that can tag agents/projects/repos uses it, roster from `useMentionOptions`), `ClarificationCard`, `ClarificationSummary`, `SessionActionCard`, `ProjectIndexStatus`, `confirm-dialog`, `AttachmentDropzone`, `AttachmentList`, `setup/SetupShell`, `setup/SetupStepList`, `setup/DesktopOnlyNotice`, `runner/BlockerNotice`, `projects/ProjectFormDialog`, `admin/StoreCredentialForm` (per-provider credential block, exported from `StoreCredentialsSection.tsx`) |
| **Organism** | Larger, often stateful feature block; composes molecules+atoms | feature dirs (`chat/`, `board/`, `workspace/`, `agent/`, `projects/`, `admin/`, `runner/`, `setup/`) | `Composer`, `MessageList`, `SessionSidebar`, `chat/LeadChatHeader`, `chat/LeadWelcome` (the lead agent's chat header and the /home welcome), `chat/LeadFlowSteps` (the delivery flow: eleven numbered steps that snake — left to right, a curve, right to left — across a 30-track grid so both rows span the full width, a vertical list below sm; human approval stages in the info tone), `PlanView`, `board/BoardTaskCard` (one board card, memoized with plain props — the page passes names/flags and the column-age/verify-window text, never closures over its state or its clock; nothing on it animates), `chat/SessionActivityPanel` (the chat page's persistent right panel: whole-session activity feed, collapsible to a 44px rail, state in `tt.chat.activityPanel.open`), `board/CodeReviewApprovals` (the task drawer's per-reviewer code review verdicts per round), `layout/CatalogSyncIndicator` (the header's spinner badge while a catalog sync adds agents or indexes skills; fed by the `useCatalogSync` hook in WorkspaceShell), `attachments/ImageLightbox` (an attachment image at full size in a dialog — the desktop shell cannot open blob: URLs in a new window), `board/TaskAgentRunsSection` (the task drawer's agent runs: shown-run header + feed, other runs, token totals), `activity/ActivityFeed` (+ `FeedItemList`), `CreateTaskDialog`, `TaskDetailDrawer`, `TaskAssigneeFields`, `ChatTaskDrawer`, `workspace/ActivityFeed` (board activity), `NewAgentDialog`, `NoProjectsNotice`, `AgentKPISection`, `MCPServerForm`, `admin/CloudAccountsCard` (connected Vercel/GCP/AWS provider accounts — verify/rename/replace/remove, "Connect" menu), `runner/LocalCliCard`, `runner/EnvironmentPreflight` (+ the pure `runner/claudeCodeConnect.ts` connect-flow helper), `setup/FirstRunChoice` (the first screen: local or an account — the account card calls `account.signIn()` and is hidden without it; the choice is written before the hand-over and cleared again if sign-in fails), `setup/TeamTemplateStep` (the "what kind of team?" step: a template preselects catalog agents, the PM is locked on, every other agent toggles through `enabled` and Confirm writes the changed agents), `setup/EnvironmentStep`/`AgentRuntimeStep`/`GitHubStep`/`FirstProjectStep`, `admin/GitHubCard`, `admin/AppStoreConnectCard`, `admin/GooglePlayCard`, `admin/StoreAppPickerDialog` (+ `StoreAppsBrowser` and the `useStoreAppListing` hook, same file), `projects/MobileStorePanel`, `operations/StoreReleaseControls` (store channel vocabulary + `ChannelPromoteButton`), the projects hub/repository/add organisms — see "Projects (hub, repository, add)" below — and the Design System tabs (`projects/designsystem/*`, see "Design systems" below) |
| **Template** | Page shell / layout that arranges organisms; provides sidebar, header, routing outlet | `components/layout/` | `WorkspaceLayout`, `WorkspaceShell`, `WorkspaceSidebar`, `WorkspaceAgentLayout`, `SettingsLayout`, `Header`, `ProtectedRoute` |
| **Page** | Route target; loads data, composes organisms in a template | `pages/` | `BoardPage`, `AgentChatPage`, `BoardSettingsPage`, `SetupPage`, `ProjectsPage` (the projects hub), `ProjectPage`, `RepositoryPage`, `AddRepositoryPage`, `AnalysisReviewPage`, `IntegrationsSettingsPage`, … |

Placement rule: **atoms only in `components/ui/`**; molecules/organisms in the closest feature directory (`chat/`, `board/`, `workspace/`, `agent/`, `activity/`, `projects/`, `admin/`, `runner/`, `setup/`) or `layout/` for structural pieces; pages in `pages/`.

## Guided first-run sequence — `/setup`

| Piece | What it is |
|---|---|
| `lib/setup.ts` | Step ids, the 3 states (`done`/`todo`/`unknown`), `activeSetupStep`/`setupComplete`/`setupNeedsWork`/`setupStepUnlocked`, `SETUP_PATH` |
| `hooks/useSetup.tsx` | `SetupProvider` (above the router, in `App.tsx`) deriving all four steps; owns the gate policy as `redirectToSetup` |
| `pages/SetupPage.tsx` | `setup/SetupShell` + `setup/SetupStepList` + the active step's body |

- **Nothing is stored.** Each step is read back from the thing itself:
  `host.preflight().ready`, the connected `claude` CLI row, `GET /v1/settings/github`,
  and a project with ≥1 linked repository. A local "step done" flag would be wrong the
  moment anything is disconnected.
- **Reuse, not reimplementation:** `runner/EnvironmentPreflight` (`onReport`),
  `runner/claudeCodeConnect` (+ `connectFailure`, and `connectStepLine` exported from
  `LocalCliCard`), `admin/GitHubCard`, `projects/ProjectFormDialog` (shared with
  `ProjectsPage`). Importing the first repository is not reimplemented here either —
  the step sends the user to the full-page `/projects/new` flow (`?project=<id>` once
  a project exists), same as `ProjectsPage`/`ProjectPage`'s "Add repository" buttons.
- **Browser:** only step 1 is `actionable: false` (the preflight probes the machine
  through the desktop bridge) → `setup/DesktopOnlyNotice`, never a dead button.
  `unknown` never renders as "not done"; the failing call's own sentence is shown.
- **Entry:** `ProtectedRoute` redirects on `redirectToSetup` (`needsWork`, not
  dismissed, after a 6 s grace so a launching supervisor is not read as "never
  started"), carrying `location.search`. `WorkspaceSidebar` shows "Finish setup"
  while `needsWork`. "I'll do this later" writes only `uiCache`'s `setup.dismissed`
  — a dismissal, never progress.

## Task assignee

| Piece | What it is |
|---|---|
| `board/TaskAssigneeFields.tsx` | Organism: the agent picker (`Bot`); renders nothing when the board has no agents. |

- Used by `CreateTaskDialog` and `TaskDetailDrawer`'s sidebar (so `ChatTaskDrawer`/`ReleasedPage`
  too). `BoardPage`'s card shows the agent badge; `created_by` shows when no agent is set.
- Clearing the assignee sends `null`.

## Analysis review — `/repositories/:repositoryId/tasks/:taskId/analysis`

The architect writes one HTML document per analysis (`TaskDocument.format: "html"`, title
`analiz: <date> <topic>`); older tasks may only have markdown `spec:`/`plan:` documents. The
reviewer highlights passages, comments on each, and sends all open comments to the agent at once.

| Piece | What it is |
|---|---|
| `pages/AnalysisReviewPage.tsx` | Full-bleed page (listed in `WorkspaceLayout`'s `fullBleed`), deliberately not another dialog inside the task drawer. Header: back link to `/board?task=<id>`, key/title, a document `Select` when there are several (`?doc=<id>`, else `lib/analysis-review`'s `pickReviewDocument`: the `analiz:` HTML doc, else any HTML doc, else the first markdown one), **Approve** (`analiz_review` only; same call as `AnalizReviewDecision`, confirmed when open comments would be left unsent) and **Send comments (N)** (N = the task's open annotations, enabled only in `analiz_review`) → `SubmitAnnotationsDialog` → `submitTaskAnnotations` → toast → `/board?task=<id>`. Body: `AnalysisFrame` (left) + `AnnotationsPanel` (right, 360px); a design task's HTML document is a canvas with `DesignPagesList` on its left. Design tasks with 2+ variants get a "Chosen: …" badge and "Choose variant" (→ `ChooseVariantDialog`) in the header, and approving with no choice is confirmed. Polls task, documents and annotations every 3s while `isRevising` (`need_revision`/`in_progress`), with an info `Notice`; one more read when the task returns to review. |
| `board/analysis/AnalysisFrame` | Organism: the document in `<iframe sandbox="allow-scripts" srcdoc>` — never `allow-same-origin` — built by `srcdoc.ts`. Markdown documents are rendered with `MarkdownContent` via `react-dom/server` (lazy-loaded, `markdownFrame.tsx`) into a themed page and go through the same sandbox. Listens to `message` only when `event.source === iframe.contentWindow` and only for shapes `parseFrameMessage` accepts; sends the frame nothing but the annotations to draw (deduplicated by content), scroll requests (`ref.scrollTo(id)`) and, with `canvas` (design documents, HTML only), zoom actions from its `CanvasZoomControls` overlay and page jumps (`ref.goToPage(id)`); it reports the document's pages (`onOutline`) and the page in view (`onPageChange`, `view` when the reviewer moved, `goto` otherwise). |
| `board/analysis/DesignPagesList` | Molecule: a canvas's pages (one per `<h2>` section) as Figma's left-hand list, the one in view highlighted; an optional `toolbar` slot (the compare view's sync switch). |
| `board/analysis/ChooseVariantDialog` | Molecule: the variant choice — every variant as a radio `Button`, the current choice badged, an optional note. Posts `Chosen variant: <title>` with the note on the lines after it (the server and the agent read only the first line as the title). |
| `board/analysis/CanvasZoomControls` | Molecule: the zoom bar over a canvas frame — out, the current level (click for 100%), in, fit to width, and a `HelpTooltip` on how to pan. |
| `board/analysis/srcdoc.ts` | `FRAME_SANDBOX`, `frameCsp(nonce)` (`default-src 'none'; style-src 'unsafe-inline'; img-src data: https:; font-src data:; script-src 'nonce-…'; form-action 'none'; base-uri 'none'`), `createNonce()` (per render), `buildAnalysisSrcdoc` (DOMParser, strips `meta[http-equiv]`/`base`/`link`/`noscript` and stray `nonce`s, CSP meta first in `<head>`, highlight styles, the runtime last in `<body>`; with `canvas: true` also `<html data-tt-canvas>` and the canvas CSS — hidden scrollbars, grab cursors, the document's own `overflow`/`scroll-behavior` on `html`/`body` overridden), `parseFrameMessage`. |
| `board/analysis/frameRuntime.ts` + `textQuote.ts` | The one script the frame runs, and the pure text-quote module it shares with the tests: selection → `{quote, prefix ≤32, suffix ≤32}` over whitespace-collapsed body text; anchoring prefers the occurrence whose context matches, falls back to the first; marks split across element boundaries. Both are serialized with `Function.prototype.toString`, so neither may reference anything outside its own body. Messages: frame → page `tt:ready`, `tt:selection`, `tt:anchored`, `tt:focus`; page → frame `tt:annotations`, `tt:scrollTo`. Links inside the document do nothing except in-page anchors. |
| `board/analysis/canvasRuntime.ts` | Serialized into the same script; a no-op unless `<html data-tt-canvas>`. Lays the document out once at the frame's width, opens page-level `overflow-x:auto` strips, and moves `<body>` with a transform (the frame never scrolls; anything that scrolls it becomes a pan): drag empty space, Space + drag, the middle button or the wheel pans, pinch / Ctrl/⌘ + wheel zooms at the pointer — dragging over text still selects it. Starts fitted to the width. Each `<h2>` starts a page (its own `<section>`, else the siblings up to the next heading); boxes are measured once in body pixels. Messages: page → frame `tt:zoom` {action}, `tt:goto` {page}, `tt:scrollTo`; frame → page `tt:zoom` {zoom}, `tt:outline` {pages}, `tt:page` {id, cause}. frameRuntime leaves scrolling to it on a canvas. |
| `board/analysis/AnnotationsPanel` | Organism: counts, the composer for a pending selection (`createTaskAnnotation`), and one `AnnotationItem` per annotation of the shown document — status badge (Open/Sent/Resolved), "not found in this version" when the frame could not re-anchor it, the agent's `reply`, edit/delete (open only, delete confirmed) and reopen (resolved). Optimistic, reverted on error. Commenting pauses while the agent revises. |
| `board/analysis/AnnotationComposer`, `AnnotationItem`, `SubmitAnnotationsDialog` | Molecules used above (`SubmitAnnotationsDialog` = `admin/FormDialog` + optional note). |
| `lib/analysis-review.ts` | `analysisReviewPath`, `documentFormat`, `pickReviewDocument`, `isRevising`, `annotationCounts`. |

- **Security invariant:** agent-written HTML is rendered only inside that frame. The app page holds the API
  token and has no CSP; nothing else may put document HTML into the app's DOM.
- Entry points: `board/AnalizReviewDecision` shows **Review analysis** (with the open-comment count) under
  Approve/Decline whenever the task has a document; `board/TaskDocumentList` opens an HTML document's card on
  this page (`onOpenReview`) instead of its markdown reader dialog.

## Board & backlog project scope

| Piece | What it is |
|---|---|
| `hooks/useProjectScope.ts` | The scope both pages share: `"all"`, `"none"` or a project id. `?project=` wins; without it, the last choice in localStorage (`tt.board.projectScope`, every access in try/catch). A valid `?project=` is remembered too, so the sidebar's plain `/board`/`/backlog` links keep it. An id that is no longer a project, or any scope while no project exists, reads as `"all"`. `setScope(next, dropParams?)` writes both, dropping other params in the same navigation. |
| `board/ProjectScopeSelect.tsx` | Molecule: the header picker, always rendered on both pages — "All projects", each project, "No project" (only when a project exists and such tasks exist or it is selected), each with the count for the page it sits on. With no project at all it offers "All projects" plus "Create a project", which navigates to `/projects` without touching the scope. |
| `lib/project-board.ts` | Pure, unit-tested rule: a task belongs to its `initiative_project_id`; without one, to every project its repository's `project_ids` lists. "No project" = neither. `filterTasksByScope`, `projectScopeCounts`, `scopeShowingTask` (the scope a `?task=` link switches to so its card is visible), `taskCreateDefaults` (under a project: `CreateTaskDialog`'s `defaultInitiativeProjectId` preselected, its repositories first and the first one preselected). |

- `BoardPage` filters every lane (column counts follow), hides the card's project badge under a
  project scope (the repository badge stays), and shows a dashed empty state with "New Task" when the
  scope has no cards. `?task=<id>` still opens the drawer: a task outside the scope switches the scope
  (`scopeShowingTask`), and a task missing from the cached board waits for the first fetch.
- `BacklogPage` uses the same hook, picker and rule (it replaced its own local project filter).
- `ProjectPage`'s header links to `/board?project=<id>` and `/backlog?project=<id>`.

## Projects (hub, repository, add)

The from-scratch Projects redesign: a projects hub, one page per project, one
tabbed page per repository, and a full-page wizard for adding repositories.
Nothing here calls the old repository-profile, repository-dependency or
pipeline-config endpoints — that model is `RepositoryModel`
(components/checks/links/scans), read through `useRepositoryModel`.

| Piece | What it is |
|---|---|
| `pages/ProjectsPage.tsx` | The hub: a Cards/Map segmented toggle (`?view=map`) over either every project as a `hub/ProjectCard` + an unassigned-repositories card + filters, or `map/WorkspaceMapView`; "Add repository" / "New project" either way. |
| `pages/ProjectPage.tsx` | One project, tabbed (`?tab=`): Architecture (`map/ProjectArchitectureMap`, the default the moment the project has a repository), Repositories table, cross-project Review queue, Design System (`?tab=design`, `designsystem/ProjectDesignSystemTab`), Settings. |
| `pages/RepositoryPage.tsx` | One repository, tabbed (`?tab=`): overview, components, checks, links, deploy, design (`designsystem/RepositoryDesignSystemTab`), settings. |
| `pages/AddRepositoryPage.tsx` | The `/projects/new` wizard, driven by `add/useAddRepositoryFlow`: Source → Scan → Review → Done for a folder/GitHub import, Source → Done for a new repository (nothing to scan yet; the stepper shows only those two). |
| `hooks/useProjectsOverview.ts` | `GET /v1/projects/overview` for the hub; cached (`CACHE_PROJECTS_OVERVIEW`), paints last snapshot on a failed refresh. |
| `hooks/useProjectOverview.ts` | `GET /v1/projects/:id/overview` for `ProjectPage`; same per-id cache-then-refresh contract as `useRepositoryModel`. |
| `hooks/useProjectMap.ts` | `GET /v1/projects/:id/map` for the Architecture tab; same per-id cache-then-refresh contract, loaded only once that tab mounts. |
| `hooks/useWorkspaceMap.ts` | `GET /v1/projects/map` for the hub's Map view; loaded only once that view is selected. |
| `hooks/useRepositoryModel.ts` | `GET /v1/repositories/:id/model` for `RepositoryPage`; per-id cached snapshot. |
| `hooks/useDesignSystem.ts` | `useProjectDesignSystem` / `useRepositoryDesignSystem`: the Design System tabs' views + generate (+ base-project choice), same per-id cache-then-refresh contract, re-polled every 10s while the opened design task is still open. |
| `hooks/useScanProgress.ts` | Polls a repository's latest scan until it finishes; used by the add-repository flow's `add/ScanStep` and `RepositoryPage`'s scan banner. |

`components/projects/hub/`: `ProjectCard`, `ProjectFilters` (role + search, kept in
the URL query), `ProjectRepositoriesTable`, `ProjectReviewTab`, `ProjectSettingsTab`,
`RepositoryOverviewRow`, `ScanStatusLabel`, `UnassignedRepositoriesCard`,
`AttentionNotice` (project-wide review shortcut), `GitWarningIcon`,
`EnvironmentChips` (one component's confirmed environments — provider mark, short
env label, health dot, error-count badge — read straight off
`RepositorySummary.environments`; used by both `RepositoryOverviewRow` and
`ProjectRepositoriesTable`, one call per component).

`components/projects/repository/`: `RepositoryHeader`, `OverviewTab`, `ComponentsTab`
(+ `ComponentRail`, `AddComponentDialog`, `ComponentPickerDialog`,
`RepositoryReviewSettings` — the repo-wide `require_human_review` toggle and the
no-CI-workflows setup notice, rendered at the top of the tab), `ChecksTab`
(+ `AddCheckDialog`), `LinksTab` (+ `AddLinkDialog`, `ExternalResourceDialog`,
`ResourcePickerDialog` — every workspace `SystemResource`, filterable by kind/
search and sectioned by repo/project/elsewhere; the picker behind both
"merge with an existing resource" and "link to an existing resource" on a
link's resource panel, and behind the duplicate-resource notice's merge flow),
`SettingsTab`, `LocalCommandsDialog`, and the
Deploy & Runtime tab — `components/projects/repository/deploy/` — below.

`components/projects/repository/deploy/`: the Deploy & Runtime tab, built on
the Phase 2 cloud accounts/environments model.

| Piece | What it is |
|---|---|
| `DeployRuntimeTab` | Organism, the tab's root: the shared `ComponentRail` (production-environment provider mark + health dot per component via `renderTrailing`) + `DeliveryCard` (every component, including mobile), then either `StoreReleasesCard` (mobile) or `EnvironmentsCard` + `RuntimePanel`, then `ReleasesCard` for every component. Owns the connected-accounts list and which environment row is selected. |
| `DeliveryCard` | Organism: the selected component's delivery profile (mode + executor + workflow, soak minutes, max new errors, smoke check count, auto-rollback on/off) — `override` badge "set by you", `detected` badge "detected (\<confidence\>)", or (no override and a detected confidence below exact/high, or nothing detected) an unconfirmed warning `Notice` with a **Confirm** button (`api.updateComponentDelivery(id, detected)`) that saves the detection as the override; **Edit** always opens `DeliveryEditDialog`. `batch`+`github_actions` shows a one-line hint that the release workflow must trigger on the tag push and read the version from the tag name. Mirrors `domain.DeliveryConfirmed` client-side. |
| `DeliveryEditDialog` | Organism/dialog: mode select; executor select filtered by mode (mirrors `ComponentDelivery.Validate`'s mode/executor pairing — on_merge: github_actions\|vercel; dispatch: github_actions; batch: github_actions\|local\|store; none: hidden); workflow (required for dispatch); tag pattern + local command (batch only); soak minutes (1–120), max new errors (≥0); a smoke-check list editor (GET\|HEAD, path starting with `/` or an absolute URL, optional expect-status/contains, max 20 — `MAX_SMOKE_CHECKS`); auto-rollback switch; "Reset to detected" sends `delivery: null`. Client-side validation mirrors the server's `Validate`; a 400's message is shown verbatim for whatever it misses. |
| `EnvironmentsCard` | Organism: production/staging/preview rows (development only when bound) for the selected component — provider, resource, URL, health dot, error count, last deploy, `suggested`/`auto` badges (a `per_branch` row shows "Auto for every PR" + the Vercel project instead of a URL, still selectable); Connect/Change (`BindEnvironmentDialog`) and Disconnect (`deleteEnvironment`, confirmed); a `noAccountsAtAll` empty state that opens `CloudAccountDialog`. A `suggested` row hands off to `model/EnvironmentCandidates` instead of the usual actions. |
| `BindEnvironmentDialog` | Organism/dialog: step 1 picks a connected account or "Custom URL"; step 2 is either a searchable, refreshable `listCloudResources` picker or a URL/health-URL pair (PREVIEW + a Vercel account adds a note that previews are per PR/branch). Submits `bindEnvironment`. |
| `RuntimePanel` | Organism: one bound environment's live picture — `getEnvironmentOverview` header (resource status, revision, console link, latest deployment), an `unavailable` `Notice` whose action is read off `unavailable_code` (`"cloud_auth"` → Reconnect, opens `CloudAccountDialog` in replace mode; `"not_connected"` → Connect, opens it in create mode preset to `env.provider`), never the message text, and pill `Tabs` for Errors/Logs/Deployments (Errors hidden when `errors_supported === false`). A `preview_access` badge (Protected (mode) / Public) and, when protected with no bypass, a warning `Notice` linking Vercel's "Protection Bypass for Automation" docs. |
| `ErrorsPanel` | 1h/24h/7d `getEnvironmentErrors` list: message (click to expand the sample), count, `new` badge, first/last seen, external link, "Create task" (`createErrorTask`) with a toast linking to the created task. |
| `LogsPanel` | Severity floor + preset/custom time range + 400ms-debounced search against `getEnvironmentLogs`; a "Live" toggle re-polls every 5s via `hooks/usePolling` (pauses on a hidden tab, stops the moment it's switched off); monospace scroll list, `next_cursor` "Load more", `truncated` notice. |
| `DeploymentsPanel` (+ `DEPLOYMENT_STATUS_VARIANT`) | `getEnvironmentDeployments` list: status, environment, `PR #n`, commit/branch/creator, ready duration, "Branch address" (`branch_url`) / "This commit" (`url`) / inspect links. |
| `ReleasesCard` | Organism, below the environment/runtime pieces for EVERY component (mobile included): the selected component's last 20 releases (`listReleases`), newest first — version (monospace), status chip, task count, created relative time, `deploy.run_url` when present. For a `batch` component, up to two rows are pinned first, independently: the draft — "Next release — N merged tasks" with a **Cut release** button (hidden at N = 0) — AND, separately, any `pending` release `deploy_release` was never attempted on (`recuttable`, e.g. after `ErrReleaseTagExists`) — its status chip plus a **Re-cut** button; both open `CutReleaseDialog` on the pinned release's id. A re-cuttable pending release does not stop a new draft from collecting the merges since. Polls every 15s while any listed release is `RELEASE_ACTIVE_STATUSES` (deploying/verifying/rolling_back/awaiting_verdict). Row click opens `ReleaseDrawer`. |
| `CutReleaseDialog` | Organism/dialog: loads `getReleaseCutPreview`; version input (prefilled with the suggestion, validated client-side to mirror `domain.ValidReleaseVersion`), a live tag preview from the profile's `tag_pattern`, the commit short sha, the carried task list (each task's `before_deploy` text shown under "Before this ships" — cutting confirms it), a notes textarea (prefilled generated markdown, editable), confirm-by-repository-name, submit → `cutRelease` → toast + refresh. Shows a 409/400 server message verbatim. |
| `ReleaseDrawer` | Organism/drawer: one release's full detail — header (version, status chip, mode/executor, commit short sha, tag), its task list (keys → open task), timeline (created/deploy started/deployed/verify until/finished), deploy status detail, health samples, smoke results, new runtime error groups, notes, early stop, verdict, the rollback block (reason, note, mechanism, revert sha, restored ref, provider/promoted deployment id, manual steps, detail), `cut_at`/`tag`/`notes` (batch, preformatted), `local_run` (argv, exit code, log path, tail in a scrollable `pre`), `store_builds` table. Actions, each behind a confirm-by-repository-name dialog: **Deploy** (`pending` + dispatchable), **Mark released** (`awaiting_verdict` or `failed`), **Roll back** (`failed`, `awaiting_verdict`, or `released` within 24h — the server's 409 message is shown verbatim rather than the client guessing the window). Exports `RELEASE_STATUS_VARIANT` and `RELEASE_ACTIVE_STATUSES`, both reused by `ReleasesCard` and the task drawer's release row. |

`components/board/TaskDetailDrawer.tsx` gained two release-related pieces (migrations
159–161), both reusing `deploy/ReleaseDrawer` rather than duplicating its rendering: a
compact "Release" row (status chip + version, or "Next release (draft)" for a batch
component's draft) that opens `ReleaseDrawer` when `listReleases(repoId, {taskId, limit:1})`
returns one; and, in the `before_deploy` field, when the text is non-empty and
`before_deploy_confirmed_at` is unset, a warning `Notice` with a **Confirm before-deploy
steps** button (a `ConfirmDialog`, then `api.confirmBeforeDeploy`) — confirmed shows
"confirmed \<relative time\>" instead. Under the PR row, `board/TaskPreviewsSection` lists the task's per-branch
preview deployments (`getTaskPreviews`): component, status, "Open" (`branch_url || url`), PR #, a "Protected"
badge; a refresh button, polling every 15s while any is queued/building; nothing at all for an empty list.
In the human UAT box, `board/TaskPreviewActions` puts an "Open preview" button per ready preview beside
`LocalPreviewPanel`'s "Run locally" (its `actions` slot); both read `hooks/useTaskPreviews`.

`components/projects/add/`: `SourceStep`, `SourceModePicker`, `NewRepositoryForm`,
`ScanStep` (+ `ScanRepoRow`), `ReviewStep` (+ `RepoReviewCard`), `DoneStep`,
`NewRepositoryDoneStep`, `GitHubRepoPicker`, and the pure `useAddRepositoryFlow`
state machine the page drives.

- **Source** — the project card, then `SourceModePicker`: three exclusive radio
  cards (folder / GitHub / new repository; icon + title + one line, no fields).
  Only the picked mode's details render below it — the folder path + Browse,
  `GitHubRepoPicker` (or the not-connected notice), or `NewRepositoryForm`
  (name checked and previewed with `sanitizeRepoName`/`repoNameError`, which
  mirror the server's `domain.SanitizeRepoName`/`NewRepoDirName` — lower-case
  `[a-z0-9._-]`, spaces → `-`, ≤100 chars; GitHub owner, "what will this be",
  role from `COMPONENT_ROLES`, stack with a `<datalist>` of suggestions, notes,
  scaffold switch, reference-doc checkboxes reusing
  `repositoryPage.components.docs*`). The primary button follows the mode:
  "Import" / "Create".
- **New repository** — `createNewRepository` → `POST /v1/repositories/new`; a
  failure shows inline under the form with Retry (a project the attempt already
  created is reused, not made twice). The one exception is the server's
  partial-create 500 (`repository "<name>" was created, but … failed`): the
  name is taken, so it shows the message with a link to the project (or
  `/projects`) instead of Retry, and Create stays disabled for that name. Success skips Scan and Review and lands
  on `NewRepositoryDoneStep`: the setup task (`/board?task=<id>`), "Open
  repository", "Open project".
- **Scan** — each `ScanRepoRow` reports a `ScanOutcome` (`pending`,
  `succeeded`, `failed`, `not_started` — no scan after 20s, `slow` — still
  running after 5 min; both timeouts are props for tests). Anything but
  `pending` is settled and a failed scan says the repo can go on unanalyzed.
  "Continue without waiting" leaves while scans still run server-side.
- **Review** — a `RepoReviewCard` only for repos whose scan succeeded; every
  other repo gets one line (not analyzed / still running / still importing /
  import failed). Finish waits only for those cards' models (a model that
  fails to load counts), so it never dead-ends.

`components/projects/model/` — shared molecules reading `RepositoryModel`/
`ProjectDetail` shapes, used across hub/repository/add: `ConfidenceBadge`,
`EnvironmentCandidates` (one ambiguous environment binding's candidate radio
list + "Use this", or — once there are no candidates left and no account for
`env.provider` — a "connect a `<Provider>` account" prompt composing
`admin/CloudAccountDialog`; shared by `ReviewList`'s environment item and the
Deploy & Runtime tab's `EnvironmentsCard`), `EvidenceList`, `FactValue`,
`LinkTargetLabel`, `ProjectTypeBadge`, `ProviderIcon` (a cloud provider's mark —
a plain lucide glyph on theme tokens, never a brand logo image; Vercel/GCP/AWS),
`RepoShapeBadge`, `ResourceKindIcon`, `ReviewList` (renders every `review` item;
`kind: "environment"` composes `EnvironmentCandidates`; `kind: "component"` —
"New component detected as `<role>` — keep it?" → Keep (`updateComponent(id,
{reviewed:true})`) / Dismiss (`{status:"dismissed"}`); `kind: "check"` — "CI now
requires `<workflow> › <job>` before hand-off" → OK (`updateCheck(id,
{reviewed:true})`) / Make informative (`{gate:"info", reviewed:true}`)),
`RoleBadge`, `ScanProgressList`.

## Design systems — project and repository "Design System" tabs

One base design system per project, plus an optional layer per repository that
adds or overrides tokens (API: `.ai/api-spec.md` "Design systems"). Versions
come from a `design` task the `ui-designer` agent runs; the human approves it
on the analysis review page like an `analiz` task.

`components/projects/designsystem/`:

| Piece | What it is |
|---|---|
| `ProjectDesignSystemTab` | Organism, the project page's tab (loaded only when mounted, via `hooks/useDesignSystem`'s `useProjectDesignSystem`): the create card (no base yet) or, once a base exists, the base's `DesignSystemVersionPanel` followed by an "Update from code" card; pending proposals, version history (history rows and pending rows select which version the panel shows, with "Show the current version" back), and `DesignSystemRepositoriesTable`. |
| `RepositoryDesignSystemTab` | Organism, the repository page's tab (`useRepositoryDesignSystem`): `DesignBaseProjectPicker` (when `project_choices.length > 1` or `base_project_id` is set), a warning `Notice` while `effective.ambiguous`, the create card when there is neither a project base nor a layer (the layer then is its whole design system), the project-base summary (links to the project's tab), the effective (merged) tokens with `DesignLintChecks` over `view.lint` (the merged checks — a layer can break a contrast pair the base had right), `DesignSystemFilesCard`, the layer's `DesignSystemVersionPanel` with its overrides, the create/update-layer card, pending layers and history. |
| `DesignSystemVersionPanel` | Organism: one version — version + `DesignSystemStatusBadge`, source task link, proposed/approved date, a layer's rationale, a `children` slot, `DesignLintChecks` over `version.lint`, then pill `Tabs`: Overview (`design_md` via `MarkdownContent`), Tokens (`DesignTokensPreview`), Components (`inventory_md`). |
| `DesignLintChecks` | Molecule: the "Checks" block — error/warning count badges, then every finding errors first (`lib/designLint`'s `sortLintFindings`), each a severity `Badge` + the sentence `lintMessage` builds from its fields; a green "No problems found" line when the list is empty or absent (the server omits an empty `lint`). |
| `DesignSystemFilesCard` | Organism: the repository's rendered files (`DESIGN.md`, `design/tokens.json`, `design/tokens.css`, `design/INVENTORY.md`). Collapsed until "Show files"; only then `useDesignSystemFiles` fetches (refresh button once loaded, retry on error). Each file: path, Copy (`lib/clipboard`'s `copyText` — Clipboard API, else a hidden textarea + `execCommand("copy")`), content in a scrollable `pre`. |
| `DesignTokensPreview` | Organism: walks a DTCG tree (`lib/designTokens.ts`) — color swatches, a type-scale sample, spacing bars, radius boxes, shadow samples, and a path → type → value table for everything else; aliases show as written (`{color.primary}`) with what they resolve to, or "Unresolved alias". Inline styles carry only each token's own value, and a color reaches `background` only if it passes `cssColor` (hex, a color function with no nested parentheses, or a named color), so token data cannot inject a `url()`. Optional `overriddenPaths` mark a layer's overrides. |
| `DesignSystemGenerateCard` | Organism: the call to action (`mode="create"` = `ui/empty-state` inside a `Card` with the explanation; `mode="update"` = compact card), an optional notes textarea sent as `notes`, then the opened task (key, title, column, "Open task" → `/board?task=` or, in `analiz_review`, "Review and approve"). Says "already running" for `created: false`; a 409 shows the no-repository `Notice` with "Add repository". |
| `DesignSystemVersionList` | Molecule: `variant="pending"` (each row has "Review and approve") or `"history"` — a `Card` of `divide-y` rows: version, status, layer/repository, source task, date, and Preview/View → `onSelect`. |
| `DesignSystemRepositoriesTable` | Molecule: the project's repositories — layer version, "Layer in review", builds on this project / another project's base / "Base project not chosen", each linking to the repository's Design System tab. |
| `DesignBaseProjectPicker` | Molecule: `ui/select` with "Automatic" + every project (with its base version); `PUT …/base-project` on change, toast on success/failure. |
| `DesignSystemStatusBadge` | Molecule: approved → success, in review → warning, superseded → secondary. |
| `DesignTaskLink` | Molecule (forwards ref, so `Button asChild` works): opens a design task's analysis review page from `repositoryId` (a version's `source_task_repository_id`, a request's `repository_id`); only without one is the href `/board?task=<id>`, with a plain click resolving the repository through `api.lookupTask(key)` first. |

`lib/designTokens.ts` (pure, unit-tested): `flattenDesignTokens` (group `$type`
inheritance, `$`-metadata skipped, alias resolution with a cycle guard),
`groupDesignTokens`, `cssColor`/`cssDimension`/`cssShadow`/`typographySample`,
`formatTokenValue`. `lib/design-system.ts`: `DESIGN_SYSTEM_TAB`,
`projectDesignSystemPath`, `repositoryDesignSystemPath`, `DESIGN_TASK_TYPE`, `isDesignTask`,
and the document title convention (unit-tested): `designDocumentKind` (`design: <screen> · <variant>`
HTML → mockup, `handoff: <screen>` markdown → handoff, `design system: <name> v<N>` → system,
`design review: <screen>` → review), `designDocumentLabel` (the title without its prefix),
`designHtmlDocuments` (mockups first, in variant order) / `designMarkdownDocuments` (hand-offs
first), and the variant choice marker: `CHOSEN_VARIANT_PREFIX` (`"Chosen variant: "`, always
English — the designer agent looks for it), `chosenVariantComment`, `chosenVariantTitle` (the
newest marker comment's title). `lib/designLint.ts` (unit-tested): `sortLintFindings`,
`lintCounts`, `lintMessage(finding, t, lang)` — code → `designSystem.checks.messages.<code>`
(an unknown code → `unknown`), the ratio formatted for the language. `hooks/useDesignSystem`
also exports `useDesignSystemFiles(repositoryId, enabled)`.

### Designs under a task

| Piece | What it is |
|---|---|
| `hooks/useTaskDesign` | `GET …/tasks/:taskId/design` once per opened task (not on the drawer's poll), then `listTaskAttachments` for each referenced design task, keeping only images (the screenshots the designer saved with `attach_to_task`). |
| `board/TaskDesignSection` | Organism in `TaskDetailDrawer`, under the technical description; renders nothing when there are no references and no design system. A design-system line ("TaskTrooper v3 · web layer v1", a warning badge while ambiguous) linking to the repository's Design System tab; then one `Card` per referenced design task: key + title → its review page, the markdown documents in an `Accordion` (hand-offs open by default, via `MarkdownContent`), the HTML documents as rows with "Open" → `DesignDocumentDialog`, and the screenshots through `attachments/AttachmentList` (blob thumbnails, never a raw `<img src>`). |
| `board/DesignDocumentDialog` | Molecule: one design document in a near-full-screen dialog, read-only, in `AnalysisFrame` as a canvas with its `DesignPagesList` (the same sandboxed frame — no annotations). |
| `board/analysis/DesignVariantCompare` | Organism on `AnalysisReviewPage`: two variants side by side (stacked below `lg`), each a canvas `AnalysisFrame` under its own document `Select` and a "Chosen" badge or "Choose this variant" (→ the page's `ChooseVariantDialog`), with one `DesignPagesList` (the left document's pages) and a "keep pages in step" switch: a page reached from the list or by panning (`cause: view`) takes the other pane to `lib/design-pages`' `matchingPage` (same heading with the variant names removed, else the same position). Both canvases take comments; the page shows `AnnotationsPanel` beside it with each comment labelled by its variant (`documentLabel`). The left/right document ids are the page's state. |

`AnalysisFrame`'s `annotations`, `activeId` and the `on*` handlers are optional: without
them it is a read-only view (no highlights, selections ignored). `AnalysisReviewPage` shows
"Compare variants" only for a design task with 2+ HTML documents (it then reads the task's
comments once); comparing replaces the single frame (the comments panel stays) and hides the
header document select, whose items also mark the chosen document.

`design` tasks elsewhere: `lib/project-board`'s `BUILT_IN_TASK_TYPES` labels
them ("Design"); everything that opens the review page is column-based
(`analiz_review`), so they get it unchanged. For a design task the wording
switches to `analysisReview.design.*`: "Review design" on
`board/AnalizReviewDecision`'s link (and its decline placeholder), and on
`AnalysisReviewPage` the empty state, revising banner, submit tooltip, toasts,
`AnalysisFrame`'s iframe `title` and `AnnotationsPanel`'s `pausedLabel` (both
optional props defaulting to the analysis wording). Both also say that
approving a design task approves the design system versions it proposed.

## Architecture & workspace maps (Phase 3)

`components/projects/map/` — React Flow (`@xyflow/react`, the one dependency
this phase added; its stylesheet is imported by each component that renders `<ReactFlow>`,
so it ships with their lazy chunks rather than the entry, and is themed off
the app's own CSS variables in `styles/globals.css`, never a hard-coded hex).

| Piece | What it is |
|---|---|
| `ProjectArchitectureMap` | Organism: one project's map (`ProjectMap` from `useProjectMap`) — a deterministic tiered layout (`lib/architectureMapLayout.ts`'s `layoutMapNodes`: client → service → library (opt-in) → data → external, grouped by repository/path within a tier), custom `ComponentMapNode`/`ResourceMapNode` types, a custom `MapEdgeLine` (confirmed solid, suggested dashed warning-colored, cross-project thicker/info-colored, protocol label on hover/selection), pan/zoom/minimap via React Flow, `MapFilters` (libraries/suggestions/other projects) and `MapSidePanel` on node/edge selection. Empty state when the project has no components. |
| `MapSidePanel` | Molecule: a node's identity + outgoing/incoming edge list (each row reselects that edge) + "Open repository" (`/repositories/:id`) / "Open project" (`/projects/:id`, foreign nodes only, resolved against the `crossProjects` prop); an edge's two ends + Confirm/Dismiss (`api.updateLink`) while it is still `suggested`. |
| `WorkspaceMapView` | Organism: the hub's Map view (`WorkspaceMap` from `useWorkspaceMap`) — `lib/workspaceMapLayout.ts`'s `buildWorkspaceMapGraph` lays out every linked project as a `ProjectGroupNode` (a React Flow parent/group node) holding its repositories as `WorkspaceRepoNode` children (role chips per component), cross-project edges from `edges` (labeled "N links" (+"suggested" count), only between projects that actually have one), `WorkspaceResourceNode`s for resources `shared_resources` says are used by ≥2 projects, and independent projects in their own grid below with an "Independent" caption and no edges at all. Clicking a project node opens `/projects/:id?tab=architecture`. |
| `lib/architectureMapLayout.ts` / `lib/workspaceMapLayout.ts` | Pure, unit-tested layout functions (`layoutMapNodes`/`isProjectLinked`/`buildWorkspaceMapGraph`) plus the React Flow node/edge adapters (`toFlowNodes`/`toFlowEdges`) — kept framework-adjacent but DOM-free so tier ordering, grouping, foreign placement and the library toggle are tested without rendering. |

React Flow needs `ResizeObserver`/`DOMMatrix` in `jsdom`; the stubs live in
`vitest.setup.ts` (shared, not per-test-file) — the `ResizeObserver` one fires
its callback synchronously from `observe()`, since a real node/handle
measurement (and therefore any edge touching that node) never happens
otherwise in a environment with no layout engine.

**Removed, not to be recreated:** `pages/ProjectSettingsPage.tsx`;
`pages/DeploySettingsPage.tsx` (+ `projects/DeploySettingsSection` and its
test — `/repositories/:id/deploy` redirects to the repository page's own
Deploy & Runtime tab instead); `projects/ProjectProfileCard`,
`DependenciesPanel`, `DependencyFormDialog`, `ProjectArchitectureSection`,
`ProjectRepositoriesSection`, `RepositoryRow`, `RepositoryDialogs`,
`useRepositoryImport`, `InitialSetupDialog`, `RepositoryAnalyzingDialog`,
`ScopeSetupFields`, `PipelineSlots`, `SubRepoSettingsPanel`;
`lib/dependencyTargets.ts`; `projects/repository/deploy/DeliverySettingsPanel`,
`projects/DeployTargetsSection` and `lib/deployTargets.ts` (the Deploy & Runtime
tab's "Delivery settings" accordion: per-env deploy targets, incident policy,
test strategy, env inventory — the Environments card covers where a component
ships); `projects/repository/KnowledgeTab` (+ `NoteDialog`) — the repository
page's former "Knowledge" tab (project notes, note topics); agents now fetch
project facts through tools themselves instead of reading a server-maintained
note. Their jobs are now either the `RepositoryModel`
tabs above, the add-repository wizard, or (for the deploy page) the Deploy &
Runtime tab.

## Store console + mobile release panel

| Piece | What it is |
|---|---|
| `admin/StoreCredentialForm` | The per-provider credential block, exported from `StoreCredentialsSection.tsx` (the old `StoreCredentialsSection` export is gone; callers render one `StoreCredentialForm` per provider). Used by `AppStoreConnectCard` / `GooglePlayCard`. |
| `admin/StoreAppPickerDialog` | Dialog to bind a repo/platform to one store app; same file also exports `StoreAppsBrowser` (lists a credential's apps) and the `useStoreAppListing` hook (on-demand fetch, `listing_available:false` ≠ error). |
| `projects/MobileStorePanel` | Organism: per-repo link/tracks/promote/build panel, composes `StoreAppPickerDialog` and the pieces below. |
| `IntegrationsSettingsPage` | Composes `AppStoreConnectCard` + `GooglePlayCard` + `CloudAccountsCard` alongside `GitHubCard` (all pasted/stored credentials). |

`StoreAppPickerDialog` answers the same three states every provider picker in
this app must: a list, "connected but this account cannot be enumerated"
(which opens the manual identifier field), and **not connected at all** (which
opens neither — there is nothing to verify a typed value against — and points
at Integrations). An empty list is a fourth, separate answer. The server
states which one it is in `reason`; never infer it from an empty array.

The store channel vocabulary (`STORE_CHANNELS`, `NEXT_STORE_CHANNEL`, the
label/badge helpers, `ChannelPromoteButton`) lives in
`operations/StoreReleaseControls` and is imported by `MobileStorePanel` —
do not redeclare that switch elsewhere.

### Store test builds and simulator runs

| Piece | What it is |
|---|---|
| `operations/storeTestBuilds.ts` | The test-build vocabulary: status → label/badge, active/actionable checks, `testBuildLabel` (same text as the server's `Label()`), group-kind labels, simulator status helpers. The only place these switches live. |
| `operations/StoreTestBuildParts` | Molecules shared by both surfaces: platform icon, status badge, copy button, external link, collapsible log tail, group chips. |
| `operations/OpenToGroupsDialog`, `operations/ExportComplianceDialog` | One component each with an `inline` mode: inline inside the task drawer (no modal inside the drawer's own modal), a dialog in the Ops drawer. The compliance answer has no default — nothing is sent until one is picked. |
| `operations/AppTestBuildsSection`, `operations/AppTestGroupsSection` (+ `NewTestGroupDialog`, `StoreTestersDialog`) | Ops → Apps drawer: the app's builds with open/close/compliance, and its TestFlight groups / Play tracks with the auto-distribute switches and testers. |
| `board/TaskMobileTesting` | Human UAT box: renders `TaskStoreBuildsPanel` (one `TaskPlatformTestBuild` per linked platform) and `SimulatorRunPanel`, or nothing for a repository with no linked app. |
| `hooks/useStoreTestBuilds`, `hooks/useStoreTestGroups` | Loaders; builds poll every 5s only while one is queued, building or processing. |

## MCP servers — Settings → MCP Servers

| Piece | What it is |
|---|---|
| `pages/MCPServersPage` | The server table, the add/edit `FormDialog`, the OAuth sign-in flow (start → open in the system browser → poll the list until `auth` is `oauth_connected`) and the agent list behind "listed by". |
| `admin/MCPServerForm` | Organism: the add/edit form; "Available to" is a `ui/select` (`listed` default for a new server, `all`), with a one-line hint per choice. |
| `admin/MCPServerTableRow` | Molecule: one row — transport, access badge + "Listed by …", status (or **Sign-in needed/expired** with Connect/Reconnect, **Signed in** with Disconnect, "Waiting for sign-in…" with a fallback link to the sign-in page), tools, enabled switch. |
| `admin/MCPOAuthClientDialog` | Molecule (`admin/FormDialog`): client id + optional secret for an authorization server without dynamic registration, showing the redirect URI to register. |
| `admin/MCPServerPicker` | Molecule used by `ToolPolicyForm`: the agent-side picker; each option says whether the server is available to all agents or only to agents that list it. |
| `lib/mcpAccess.ts` | `agentsListingServer`, `needsOAuthSignIn`, `openAuthorizationPage` (desktop bridge `openExternal`, else `window.open`). No new bridge method. |

## Cloud accounts & environments (Phase 2)

Replaced the old one-account-each `admin/VercelCard` (token + per-app hosting
links) and `admin/GoogleCloudCard` (service-account JSON + bound Cloud
Run/GKE resource): a repository's components now bind to a provider-neutral
`ComponentEnvironment` (`server/internal/domain/cloud.go`,
`RepositoryModel.environments`), read through one or more `CloudAccount`s a
person connects once, on Settings → Integrations.

| Piece | What it is |
|---|---|
| `admin/CloudAccountsCard` | Organism: every connected Vercel/GCP/AWS account — provider icon, meta identity, status badge (ok/error/unverified), verified-at; Verify/Rename/Replace credential/Remove per row (the last via `ui/confirm-dialog`); a "Connect" menu opens `CloudAccountDialog` preset to one of the three providers. |
| `admin/UsageDailyChart` | Organism: Settings → Usage's daily column chart — one column per calendar day (stacked, bottom series first, 2px surface gaps, slivers under 1% left to the tooltip), clean-rounded y-axis, per-column hover/arrow-key tooltip, and an `sr-only` table of every value. Colors come from the `--chart-1..4` theme tokens (`bg-chart-N`); helpers are in `lib/usage.ts` (`breakdown`, `fillDays`, `formatCompact`, …, unit-tested). |
| `operations/LiveEnvironmentCard` / `operations/DeployFeedTable` (+ `DeployStateBadge`) | Organisms of Operations → Deployments: one confirmed environment and what it serves right now (newest deployed change — a release under verification counts — plus an in-flight marker), and the deploy table (TaskTrooper releases open `ReleaseDrawer`, provider deployments link out). The page merges `GET /v1/releases`, `/v1/projects/overview` and each in-view environment's provider deployments through the pure, unit-tested `lib/deployFeed.ts` (`buildCatalog`, `buildFeed` folds a provider deployment into the release with the same commit, `liveOf`, project/repo/environment/state filters, `mergeReleasePages`). The legacy `DeploymentMatrix` shows only when a manual GitHub Actions target exists. |
| `admin/CloudAccountDialog` | Molecule: connects a new account or replaces an existing one's credential — same per-provider fields either way, since a saved credential never round-trips. Vercel: token + optional team id. GCP: a pasted service-account JSON textarea, or a file picker that reads a `.json` key into it. AWS: access key id, secret access key, optional session token, region (`Select` of common regions + free text). Shows the 400 `{code:"cloud_auth"}` inline (`isCloudAuthError` in `api.ts`) instead of a toast, so a bad credential does not read as some other failure. |
| `projects/model/ProviderIcon` | See "Projects (hub, repository, add)" above. |
| `projects/hub/EnvironmentChips` | See "Projects (hub, repository, add)" above. |
| `projects/model/ReviewList`'s `kind: "environment"` item | "Where does `<component>` run in `<environment>`?" plus `projects/model/EnvironmentCandidates` — a candidate radio list ("Use this" → `patchEnvironment(id, {status:"confirmed", account_id, resource})`) when the scan found ambiguous matches, or a "connect a `<Provider>` account" prompt (opens `CloudAccountDialog`, `onChanged` on success) when no account is connected yet; always "Ignore" (`projectModel.review.dismiss`, reused rather than redeclared). |
| `projects/repository/deploy/*` | The repository page's Deploy & Runtime tab — environments, live runtime (errors/logs/deployments), delivery settings. See "Projects (hub, repository, add)" above. |

**Removed in Phase 2, not to be recreated:** `admin/VercelCard` (+ its
`AppLinksPanel`), `admin/GoogleCloudCard`, `admin/VercelProjectPickerDialog`
(+ `useVercelProjectListing`), `admin/GCloudResourcePickerDialog` (+
`GCloudResourcesBrowser`, `GKEWorkloadsNotice`, `useGCloudResourceListing`),
`projects/hosting/*` (`HostingPanel`, `VercelHostingBody`, `GCloudHostingBody`,
`HostingFields`). Their job is now `CloudAccountsCard`/`CloudAccountDialog`
above, plus the repository page's Deploy & Runtime tab
(`projects/repository/deploy/*`), built on `ComponentEnvironment`/
`EnvironmentRuntime`.

## Standard building blocks (use these, don't reinvent)

- **Page heading + actions** → `admin/PageHeader` (title, description, `action` slot).
- **Page body wrapper / max-width + spacing** → `layout/PageContent` (pass `className="space-y-6 pb-8"` for multi-section pages).
- **Buttons** → `ui/button` (`variant`, `size`, `asChild`). Never a raw styled `<button>` except tiny inline icon toggles.
- **Cards / panels** → `ui/card`.
- **Status pills** → `ui/badge`.
- **Forms** → `ui/input`, `ui/label`, `ui/select`, `ui/switch`, `ui/checkbox`, `ui/textarea`; dialogs via `ui/dialog` (+ `DialogHeader/Footer/Title`) or `admin/FormDialog`.
- **Destructive confirm** → `ui/confirm-dialog`.
- **Empty states** → `ui/empty-state`.
- **Loading** → `ui/skeleton` / `ui/spinner`.
- **Infinite animations** (`animate-spin`/`-pulse`/`-bounce`/`-ping`) keep the compositor drawing every frame. `styles/globals.css` pauses them under reduced motion and under `html[data-idle]`, which `lib/idle.ts` (started in `main.tsx`) sets on blur, a hidden document, or 60s without input. Use one only for something the user is watching right now (a pending button, the open drawer/chat); never on a board card or anything else that stays on screen for hours.
- **A live duration** → `ui/elapsed`, so the tick re-renders that span, not the card around it.
- **Binary attachments (images/documents)** → `attachments/AttachmentDropzone` (hidden input + button, drag-over highlight, paste-to-upload; `variant="button"` for a bare picker button; exports `uploadAttachmentFiles`) and `attachments/AttachmentList` (image blob thumbnails / file chips, click-to-open, optional `onRemove`, `compact` for chips). Never point `<img src>` at `/v1/attachments/{id}` — it needs the Authorization header; use `attachments/useAttachmentBlob`.
- **List rows** → bordered `divide-y` container with row `px-4 py-3` (see `BoardSettingsPage` columns list) — extract to a molecule if it recurs.
- **Sidebar links** → `layout/SidebarNavLink`.
- **Agent avatar / initials** → `agent/AgentAvatar`; the lead agent (resolved by `lib/leadAgent`) always gets `lead`.

## Data loading (page fetches)

| Rule | How |
|------|-----|
| Never blank a page on refresh | `setLoading(true)` only on first mount — use `useFirstLoad(...keys)` (`hooks/useCachedState`) |
| Paint before the network answers | Hold fetched payloads in `useCachedState(key, fallback)`; keys for board/backlog data live in `lib/project-board` |
| Failed refresh | Toast and keep the last good data; never `setX([])` |
| Board-shaped lists | Merge with `mergeTaskList` so unchanged rows keep object identity (an open drawer refetches on `task` identity change) |
| Live updates | `usePolling(fn, ms, enabled)` — polls pause on a hidden tab, and a tick is skipped while the previous promise is pending; `{ leading: false }` when the caller already loaded on its own. Never a raw `setInterval` for a fetch |
| One endpoint, several pollers | `hooks/useSharedPoll` — one module-level timer and one request in flight per endpoint (`ALL_TASKS_POLL`: notifications 15s, board 5s while a card is in progress or an agent runs and 15s otherwise, backlog 8s; `ACTIVITY_POLL`: notifications 15s, board badges and activity dialog 2s while an agent runs and 15s otherwise — `boardPollIntervals` in `lib/project-board`), run at the shortest interval subscribed, so a fast subscriber speeds up every other one; `begin` hands a page's version back with the value |
| Unchanged answers | Keep the previous value so React re-renders nothing: `lib/stableState` (`keepRows` by a key, `keepEqual` for plain payloads, `keepMap`), `mergeTaskList` for task lists, `keepMessages`/`mergeServerMessages` for a chat transcript |
| Mutations | Update local state first, send the request, replace with the server's answer, revert on error; bump a version ref so an in-flight refresh started before the change is discarded |
| Request deadlines | `api.ts` times out reads at 20s and writes at 180s |

## Safe updates (change a shared component without breaking the app)

Shared components have many callers. When editing one:

1. **Prefer additive, backward-compatible changes.** New props must have defaults; do not change existing prop names, types, or default behavior.
2. **Before any breaking change** (rename/remove a prop, change a default, change DOM/markup that callers style): run `grep -rn "<ComponentName" src` (and the import) to list every caller, and update all of them in the same change. Build + typecheck must pass.
3. Style-only tweaks are usually safe, but check that no caller depends on the old spacing/size (e.g., a caller that added compensating margins).
4. Keep the component's responsibility single. If a change only serves one caller, add a prop (opt-in) rather than changing the default for everyone.
5. After editing, `npx tsc --noEmit` and `npm run build` must be green — CI runs exactly those two.

## Where this is enforced

This file is linked from `CLAUDE.md`. Any UI task must follow it: reuse existing components, place new ones at the right level, and update shared components safely.

## RAG / index map

`pages/FilesPage` hosts the embedding map panel; its pieces live in `components/rag/`.

| Piece | What it is |
|---|---|
| `rag/EmbeddingMapPanel.tsx` | Organism: loads sources and chunks, runs the UMAP+clustering worker, owns the "color by" mode and the advanced projection accordion. |
| `rag/EmbeddingMapSummary.tsx` | Molecule: stat tiles, file-kind composition bar, stale-model and mock/generated warnings. |
| `rag/EmbeddingMapSearchSummary.tsx` | Molecule: code-search hits placed on the map, grouped by the active color mode, with a clear action. |
| `rag/EmbeddingGroupDetail.tsx` | Molecule: detail card for the pinned group (share, top files, sample chunks). |
| `rag/EmbeddingMapLegend.tsx` | Molecule: clickable legend rows with shares; hover highlights, click pins. |
| `rag/EmbeddingScatterCanvas.tsx` | Canvas scatter with zoom/pan, hover tooltip and direct topic labels. |
| `rag/embeddingMap.worker.ts` | Worker: UMAP layout then HDBSCAN clusters, off the main thread. |
| `lib/embeddingMap.ts` | Group color scale, worker protocol, viewport helpers. |
| `lib/embeddingMapClusters.ts`, `lib/embeddingMapTopics.ts` | Pure: clustering, chunk kinds, topic terms, directory helpers. |

## Agent activity feed

One feed, two places: the task drawer's runs section and the chat page's right panel
(`chat/SessionActivityPanel`). `components/activity/` holds the pieces; the model is pure.

| Piece | What it is |
|---|---|
| `lib/activityFeed.ts` | Pure: `buildActivityFeed(runs)` folds every run's `SessionStep`s (oldest → newest) into one `ActivityFeed` — `user`/`say`/grouped `tools`/`lane`/`event`/`run` items, nested sub-agent and sub-task lanes (`parent_call_id`, `task_key`, else a lone open subtask by position), collapsed start/end pairs, `current` ("what is it doing now"), stats. Also `classifyTool`, `toolTarget`, `isRunLive` (the live/terminal rule, shared with `useRunActivity`), `mergeSteps` (incremental poll merge). |
| `lib/activityFeedLabels.ts` | `t`-taking label helpers (`toolLabel`, `toolGroupLabel`, `currentLabel`, `eventLabel`, `laneTitle`, `statusLabel`) and time formatters; strings live in the `activityArea` locale namespace. |
| `hooks/useRunActivity` | One run: incremental `?since=` polling, plan fetched only once a plan step exists. |
| `hooks/useSessionActivity` | A whole session: per-run cache, live runs polled incrementally, settled runs never again. |
| `hooks/useStickToBottom` | Keeps a scroll container pinned to the bottom only while the reader is within 48px of it; the returned `pin()` re-sticks on purpose (the chat `MessageList` calls it when the reader sends). |
| `activity/ActivityFeed` | Organism: sub-agent chips, the rail of items, live tail, raw-steps toggle. Lanes are collapsed once completed. |
| `activity/AgentRunHeader` | Molecule: agent, status badge, ticking elapsed, "now" line, facts; `actions` slot. |
