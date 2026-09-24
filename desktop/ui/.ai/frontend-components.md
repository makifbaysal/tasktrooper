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
| **Atom** | Single-purpose primitive, no business logic, style-only | `components/ui/` | `button`, `card`, `badge`, `input`, `label`, `checkbox`, `switch`, `select`, `textarea`, `dialog`, `separator`, `skeleton`, `spinner`, `progress`, `scroll-area`, `empty-state` |
| **Molecule** | Small composition of atoms, reusable, little/no state | `components/admin/`, `components/layout/`, `components/markdown/`, `components/attachments/`, feature dirs | `PageHeader`, `FormDialog`, `KeyValueEditor`, `MultiSelectPicker`, `ToolPolicyForm`, `PageContent`, `SidebarNavLink`, `HealthStatus`, `MarkdownContent`, `MarkdownField`, `ActivityFeedItem`, `WizardStepper`, `TypingIndicator`, `ClarificationCard`, `ClarificationSummary`, `SessionActionCard`, `ProjectIndexStatus`, `TaskRunSteps`, `AgentStepList` (+ the graph node parts in `chat/AgentSteps.tsx`), `confirm-dialog`, `AttachmentDropzone`, `AttachmentList`, `setup/SetupShell`, `setup/SetupStepList`, `setup/DesktopOnlyNotice`, `runner/BlockerNotice`, `projects/ProjectFormDialog`, `admin/StoreCredentialForm` (per-provider credential block, exported from `StoreCredentialsSection.tsx`) |
| **Organism** | Larger, often stateful feature block; composes molecules+atoms | feature dirs (`chat/`, `board/`, `workspace/`, `agent/`, `projects/`, `admin/`, `runner/`, `setup/`) | `Composer`, `MessageList`, `SessionSidebar`, `ActivityPanel`, `PlanView`, `SessionGraphView`, `CreateTaskDialog`, `TaskDetailDrawer`, `TaskAssigneeFields`, `ChatTaskDrawer`, `ActivityFeed`, `NewAgentDialog`, `NoProjectsNotice`, `AgentKPISection`, `MCPServerForm`, `admin/CloudAccountsCard` (connected Vercel/GCP/AWS provider accounts — verify/rename/replace/remove, "Connect" menu), `runner/LocalCliCard`, `runner/EnvironmentPreflight` (+ the pure `runner/claudeCodeConnect.ts` connect-flow helper), `setup/EnvironmentStep`/`ClaudeCodeStep`/`GitHubStep`/`FirstProjectStep`, `admin/GitHubCard`, `admin/AppStoreConnectCard`, `admin/GooglePlayCard`, `admin/StoreAppPickerDialog` (+ `StoreAppsBrowser` and the `useStoreAppListing` hook, same file), `projects/MobileStorePanel`, `operations/StoreReleaseControls` (store channel vocabulary + `ChannelPromoteButton`), and the projects hub/repository/add organisms — see "Projects (hub, repository, add)" below |
| **Template** | Page shell / layout that arranges organisms; provides sidebar, header, routing outlet | `components/layout/` | `WorkspaceLayout`, `WorkspaceShell`, `WorkspaceSidebar`, `WorkspaceAgentLayout`, `SettingsLayout`, `Header`, `ProtectedRoute` |
| **Page** | Route target; loads data, composes organisms in a template | `pages/` | `BoardPage`, `AgentChatPage`, `BoardSettingsPage`, `SetupPage`, `ProjectsPage` (the projects hub), `ProjectPage`, `RepositoryPage`, `AddRepositoryPage`, `IntegrationsSettingsPage`, … |

Placement rule: **atoms only in `components/ui/`**; molecules/organisms in the closest feature directory (`chat/`, `board/`, `workspace/`, `agent/`, `projects/`, `admin/`, `runner/`, `setup/`) or `layout/` for structural pieces; pages in `pages/`.

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

## Projects (hub, repository, add)

The from-scratch Projects redesign: a projects hub, one page per project, one
tabbed page per repository, and a full-page wizard for adding repositories.
Nothing here calls the old repository-profile, repository-dependency or
pipeline-config endpoints — that model is `RepositoryModel`
(components/checks/links/notes/scans), read through `useRepositoryModel`.

| Piece | What it is |
|---|---|
| `pages/ProjectsPage.tsx` | The hub: every project as a `hub/ProjectCard`, an unassigned-repositories card, filters, "Add repository" / "New project". |
| `pages/ProjectPage.tsx` | One project: its repositories table, cross-project review queue, settings tab. |
| `pages/RepositoryPage.tsx` | One repository, tabbed (`?tab=`): overview, components, checks, links, deploy, knowledge, settings. |
| `pages/AddRepositoryPage.tsx` | The `/projects/new` wizard: Source → Scan → Review → Done, driven by `add/useAddRepositoryFlow`. |
| `hooks/useProjectsOverview.ts` | `GET /v1/projects/overview` for the hub; cached (`CACHE_PROJECTS_OVERVIEW`), paints last snapshot on a failed refresh. |
| `hooks/useProjectOverview.ts` | `GET /v1/projects/:id/overview` for `ProjectPage`; same per-id cache-then-refresh contract as `useRepositoryModel`. |
| `hooks/useRepositoryModel.ts` | `GET /v1/repositories/:id/model` for `RepositoryPage`; per-id cached snapshot. |
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
(+ `ComponentRail`, `AddComponentDialog`, `ComponentPickerDialog`), `ChecksTab`
(+ `AddCheckDialog`), `LinksTab` (+ `AddLinkDialog`, `ExternalResourceDialog`),
`KnowledgeTab` (+ `NoteDialog`), `SettingsTab`, `LocalCommandsDialog`, and the
Deploy & Runtime tab — `components/projects/repository/deploy/` — below.

`components/projects/repository/deploy/`: the Deploy & Runtime tab, built on
the Phase 2 cloud accounts/environments model.

| Piece | What it is |
|---|---|
| `DeployRuntimeTab` | Organism, the tab's root: the shared `ComponentRail` (production-environment provider mark + health dot per component via `renderTrailing`) + the selected component's `EnvironmentsCard`, `RuntimePanel` and the collapsed `DeliverySettingsPanel`. Owns the connected-accounts list and which environment row is selected. |
| `EnvironmentsCard` | Organism: production/staging/preview rows (development only when bound) for the selected component — provider, resource, URL, health dot, error count, last deploy, `suggested`/`auto` badges; Connect/Change (`BindEnvironmentDialog`) and Disconnect (`deleteEnvironment`, confirmed); a `noAccountsAtAll` empty state that opens `CloudAccountDialog`. A `suggested` row hands off to `model/EnvironmentCandidates` instead of the usual actions. |
| `BindEnvironmentDialog` | Organism/dialog: step 1 picks a connected account or "Custom URL"; step 2 is either a searchable, refreshable `listCloudResources` picker or a URL/health-URL pair. Submits `bindEnvironment`. |
| `RuntimePanel` | Organism: one bound environment's live picture — `getEnvironmentOverview` header (resource status, revision, console link, latest deployment), an `unavailable` `Notice` with a heuristic "Reconnect" (opens `CloudAccountDialog` in replace mode) when the message reads like an auth failure, and pill `Tabs` for Errors/Logs/Deployments. Only mounted when the environment has an account behind it. |
| `ErrorsPanel` | 1h/24h/7d `getEnvironmentErrors` list: message (click to expand the sample), count, `new` badge, first/last seen, external link, "Create task" (`createErrorTask`) with a toast linking to the created task. |
| `LogsPanel` | Severity floor + preset/custom time range + 400ms-debounced search against `getEnvironmentLogs`; a "Live" toggle re-polls every 5s via `hooks/usePolling` (pauses on a hidden tab, stops the moment it's switched off); monospace scroll list, `next_cursor` "Load more", `truncated` notice. |
| `DeploymentsPanel` (+ `DEPLOYMENT_STATUS_VARIANT`) | `getEnvironmentDeployments` list: status, environment, commit/branch/creator, ready duration, inspect link. |
| `DeliverySettingsPanel` | Organism: the still-relevant half of the old `DeploySettingsSection` (mobile store panel, `DeployTargetsSection`, incident policy, test strategy, env inventory), scoped to the rail's selected component instead of asking its own scope question — composes `DeploySettingsSection`'s sub-organisms directly rather than mounting it, since the tab already has a scope picker. Rendered inside a collapsible `Accordion` ("Delivery settings"). |

`components/projects/add/`: `SourceStep`, `ScanStep` (+ `ScanRepoRow`), `ReviewStep`
(+ `RepoReviewCard`), `DoneStep`, `GitHubRepoPicker`, and the pure
`useAddRepositoryFlow` state machine the page drives.

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
`kind: "environment"` composes `EnvironmentCandidates`), `RoleBadge`,
`ScanProgressList`.

**Removed, not to be recreated:** `pages/ProjectSettingsPage.tsx`;
`projects/ProjectProfileCard`, `DependenciesPanel`, `DependencyFormDialog`,
`ProjectArchitectureSection`, `ProjectRepositoriesSection`, `RepositoryRow`,
`RepositoryDialogs`, `useRepositoryImport`, `InitialSetupDialog`,
`RepositoryAnalyzingDialog`, `ScopeSetupFields`, `PipelineSlots`,
`SubRepoSettingsPanel`; `lib/dependencyTargets.ts`. Their jobs are now either the
`RepositoryModel` tabs above, or the add-repository wizard.

## Store console + mobile release panel

| Piece | What it is |
|---|---|
| `admin/StoreCredentialForm` | The per-provider credential block, exported from `StoreCredentialsSection.tsx` (the old `StoreCredentialsSection` export is gone; callers render one `StoreCredentialForm` per provider). Used by `AppStoreConnectCard` / `GooglePlayCard`. |
| `admin/StoreAppPickerDialog` | Dialog to bind a repo/platform to one store app; same file also exports `StoreAppsBrowser` (lists a credential's apps) and the `useStoreAppListing` hook (on-demand fetch, `listing_available:false` ≠ error). |
| `projects/MobileStorePanel` | Organism: per-repo link/tracks/promote/build panel, composes `StoreAppPickerDialog` and the pieces below. |
| `IntegrationsSettingsPage` | Composes `AppStoreConnectCard` + `GooglePlayCard` + `CloudAccountsCard` alongside `GitHubCard` (all pasted/stored credentials). |
| `projects/DeploySettingsSection` | Organism: one repository's whole deploy story — the scope picker (repository or sub-project), per-environment targets, incident policy, test strategy, env inventory. A mobile scope also gets `MobileStorePanel`. Mounted at the standalone `/repositories/:id/deploy` page the operations matrix links to; the repository page's own Deploy & Runtime tab (`projects/repository/deploy/DeployRuntimeTab`, below) composes its sub-organisms directly instead, scoped to the tab's own selected component. |

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
- **Binary attachments (images/documents)** → `attachments/AttachmentDropzone` (hidden input + button, drag-over highlight, paste-to-upload; `variant="button"` for a bare picker button; exports `uploadAttachmentFiles`) and `attachments/AttachmentList` (image blob thumbnails / file chips, click-to-open, optional `onRemove`, `compact` for chips). Never point `<img src>` at `/v1/attachments/{id}` — it needs the Authorization header; use `attachments/useAttachmentBlob`.
- **List rows** → bordered `divide-y` container with row `px-4 py-3` (see `BoardSettingsPage` columns list) — extract to a molecule if it recurs.
- **Sidebar links** → `layout/SidebarNavLink`.

## Data loading (page fetches)

| Rule | How |
|------|-----|
| Never blank a page on refresh | `setLoading(true)` only on first mount — use `useFirstLoad(...keys)` (`hooks/useCachedState`) |
| Paint before the network answers | Hold fetched payloads in `useCachedState(key, fallback)`; keys for board/backlog data live in `lib/project-board` |
| Failed refresh | Toast and keep the last good data; never `setX([])` |
| Board-shaped lists | Merge with `mergeTaskList` so unchanged rows keep object identity (an open drawer refetches on `task` identity change) |
| Live updates | `usePolling(fn, ms, enabled)` — board 5s, backlog 8s; polls pause on a hidden tab |
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
