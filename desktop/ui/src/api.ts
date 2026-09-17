import { apiUrl } from "@/lib/apiBase";
import { getApiToken } from "@/lib/auth";
import { createChatStreamDecoder, type ChatStreamEvent } from "@/lib/sse";

export interface ToolPolicy {
  allow_mcp_servers?: string[];
  allow_tools?: string[];
}

export interface Session {
  id: string;
  title: string;
  model: string;
  workspace_dir?: string;
  project_id?: string;
  agent_id?: string;
  created_at: string;
  updated_at: string;
  expires_at?: string;
}

export type TaskColumn = string;

export interface BoardSettings {
  key_prefix: string;
}

export interface BoardColumn {
  id: string;
  slug: string;
  label: string;
  position: number;
  is_backlog: boolean;
}

export interface BoardMember {
  agent_id: string;
}

export interface BoardSubscription {
  agent_id: string;
  column_slug: string;
}

export interface BoardTransition {
  from: string;
  to: string;
}

export interface WorkspaceConfig {
  settings: BoardSettings;
  columns: BoardColumn[];
  members: BoardMember[];
  subscriptions: BoardSubscription[];
  transitions: BoardTransition[];
}

export interface ActivityItem {
  id: string;
  kind: "board_event" | "agent_run";
  project_id?: string;
  task_id?: string;
  agent_id?: string;
  status?: string;
  summary?: string;
  event_type?: string;
  payload?: Record<string, unknown>;
  created_at: string;
}

export interface TaskComment {
  id: string;
  task_id: string;
  author_type: string;
  author_id: string;
  author_name?: string;
  content: string;
  created_at: string;
}

export type LocalPreviewStatus = "starting" | "running" | "stopped" | "failed";

/** One repository's "run it locally" process — see localpreview.Service. */
export interface LocalPreview {
  repository_id: string;
  task_id: string;
  branch: string;
  command: string;
  status: LocalPreviewStatus;
  url?: string;
  detail?: string;
  started_at: string;
  log_tail?: string[];
}

export interface TaskAgentRun {
  id: string;
  task_id: string;
  agent_id: string;
  board_event_id: string;
  session_run_id?: string;
  status: string;
  summary: string;
  /** Token spend summed over every LLM call the run made. prompt_tokens is the
   * TOTAL prompt size; the cache counters are subsets of it. Optional so a
   * frontend deployed ahead of the server reads them as absent, not NaN. */
  llm_calls?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  created_at: string;
  updated_at: string;
}

/** One board event in a task's history (created / moved / assigned / commented). */
export interface TaskEvent {
  id: string;
  repository_id: string;
  task_id: string;
  event_type: string;
  payload?: Record<string, unknown>;
  created_at: string;
}

export interface TaskPipelineJob {
  id: string;
  pipeline_id: string;
  name: string;
  command: string;
  status: "pending" | "running" | "success" | "failed" | "skipped";
  exit_code?: number;
  output?: string;
  duration_ms: number;
  position: number;
  run_url?: string;
  coverage_pct?: number;
}

// PipelineGateReason is why the code-review gate let a reviewer through with no
// green build behind it. It is a closed set on the server
// (domain.PipelineGateReason*), and the UI must render a sentence for each:
//
//   timeout          — nothing reported inside the gate window
//   ci_unavailable   — GitHub refused to run (quota/billing) or no run exists
//   no_ci_configured — this repository has no build/test job mapped
//   gate_disabled    — require_pipeline_for_review is off for this repository
export type PipelineGateReason = "timeout" | "ci_unavailable" | "no_ci_configured" | "gate_disabled";

export interface TaskPipeline {
  id: string;
  task_id: string;
  repository_id: string;
  trigger: "ready_for_qa" | "manual" | "retry" | "stage_deploy" | "preprod_deploy" | "prod_deploy";
  // "skipped": nothing ran (no job/workflow mapped). It passes the gate like a
  // success but proves nothing, so it must never render as green.
  status: "pending" | "running" | "success" | "failed" | "skipped";
  provider?: "github_actions" | "none" | "";
  note?: string;
  // gate_reason is why the code-review gate opened WITHOUT a green build: the
  // board stopped waiting (timeout), GitHub said the run could not happen
  // (ci_unavailable), or nothing was configured to run (no_ci_configured).
  // Empty on every ordinary pipeline.
  gate_reason?: PipelineGateReason;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  jobs?: TaskPipelineJob[];
  coverage_pct?: number;
}

/**
 * A restore fetches a registered repository's code from its recorded remote
 * onto this machine — the case where the workspace moved and every recorded
 * folder is on the old one.
 *
 * A clone is minutes long, so the request that starts it returns immediately
 * and this is how it is watched: poll the repository until `status` leaves
 * "running".
 */
export interface RepositoryRestore {
  status: "running" | "completed" | "failed";
  /** Where the checkout is being placed, in this runtime's own layout. */
  root_path?: string;
  /** git's own words when it failed — shown verbatim, it is the diagnosis. */
  error?: string;
  started_at: string;
  finished_at?: string;
}

export interface Repository {
  id: string;
  name: string;
  description: string;
  root_path: string;
  project_ids?: string[];
  git_warning?: string;
  /**
   * The server's answer to "may this project's code be fetched onto this
   * machine" — true only when the folder is genuinely
   * missing here AND a git remote is on record. Never re-derive it from
   * `git_warning`: the rule lives on the server (domain.CanRestoreWorkingCopy)
   * and a folder that exists, or holds something that is not a repository, must
   * never be offered a clone.
   */
  git_restorable?: boolean;
  /** The running or last restore attempt on the server; absent when none ran. */
  git_restore?: RepositoryRestore;
  verify_command?: string;
  build_command?: string;
  test_command?: string;
  kind?: RepoKind;
  sub_repo_kinds?: string[];
  sub_projects?: RepoSubProject[];
  auto_release_on_done?: boolean;
  require_human_review?: boolean;
  require_review_chain?: boolean;
  require_release_deploy?: boolean;
  // require_pipeline_for_review defaults to TRUE on the server, unlike the two
  // gates above: it is not a new requirement being opted into but existing
  // behaviour being made opt-OUT-able. Read `?? true`, never `?? false`.
  require_pipeline_for_review?: boolean;
  // Whether the repo's WHOLE-REPO coverage figure blocks a task, and the bar it
  // blocks under (0 / absent means the 90% default). Off by default: overall
  // coverage describes the codebase rather than the change, so gating on it
  // stops every task in a repo that has not reached the bar yet. The always-on
  // gate is new-code coverage, which is not per-repository and not settable.
  require_overall_coverage?: boolean;
  coverage_threshold?: number;
  // Repo-level mutation-test gate: whether it blocks a task, and the bar it
  // blocks under (0 / absent means the server default). Independent of
  // require_overall_coverage/coverage_threshold above, which gate line
  // coverage rather than mutation score.
  mutation_enabled?: boolean;
  mutation_threshold?: number;
  incident_policy?: IncidentPolicy;
  test_strategy?: TestStrategy;
  docs?: RepositoryDocs;
  webhook_installed?: boolean;
  profile_md?: string;
  profile_updated_at?: string;
  mobile_platform?: MobilePlatform;
  release_engine?: ReleaseEngine;
  created_at: string;
  updated_at: string;
}

export interface ProfileEvidence {
  path: string;
  line?: number;
  note?: string;
}

/**
 * A profile section is either `derived` — collected from the tree by the
 * backend's parser (stack, commands, CI, deploy, git workflow) — or `agent`,
 * the judgment half, which only exists with evidence paths behind it.
 */
export type ProfileSectionOrigin = "derived" | "agent";

export interface ProfileSection {
  id: string;
  repository_id: string;
  section: string;
  body_md: string;
  evidence?: ProfileEvidence[];
  source_paths?: string[];
  source_commit?: string;
  origin: ProfileSectionOrigin;
  stale: boolean;
  updated_at: string;
}

export type ProfileProposalStatus = "pending" | "applied" | "dismissed";

/** A repository setting the profiling pass worked out from the tree. */
export interface ProfileProposal {
  id: string;
  repository_id: string;
  field: string;
  slot?: string;
  value: unknown;
  current?: string;
  label: string;
  evidence?: ProfileEvidence[];
  status: ProfileProposalStatus;
  created_at: string;
  applied_at?: string;
}

export interface RepositoryProfile {
  profile_md: string;
  profile_updated_at: string | null;
  sections?: ProfileSection[];
  proposals?: ProfileProposal[];
}

export type DeployEnv = "local" | "stage" | "preprod" | "prod";

export type TestStrategy = "local" | "stage" | "per_step";

export interface EnvFileInventory {
  path: string;
  keys: string[];
}

export interface EnvInventory {
  files: EnvFileInventory[] | null;
  keys: string[] | null;
  targets?: DeployTarget[] | null;
}

export type DeployProvider =
  | "gcp_cloud_run"
  | "gcp_gke"
  | "aws_ecs"
  | "aws_lambda"
  | "vercel"
  | "fly"
  | "app_store"
  | "google_play"
  | "custom";

export interface DeployTemplateVar {
  key: string;
  label: string;
  example?: string;
  required: boolean;
}

export interface DeployTemplate {
  id: string;
  provider: DeployProvider;
  name: string;
  summary: string;
  kinds?: string[];
  envs?: DeployEnv[];
  required_vars?: DeployTemplateVar[];
  workflow_file: string;
  rollback_hint?: string;
  body?: string;
}

export interface DeployTarget {
  id: string;
  repository_id: string;
  /** "" = the repository itself; identifies which sub-project this target ships. */
  sub_project_path?: string;
  env: DeployEnv;
  provider: DeployProvider;
  template_id?: string;
  vars?: Record<string, string>;
  health_url?: string;
  /**
   * Recent-logs endpoint the application itself serves — the other half of
   * `health_url`, read on demand by the deploy watch rather than polled.
   *
   * Optional for two reasons that look the same on the wire but are not: the
   * server omits it when it is empty (`omitempty`), AND a server older than
   * the field never sends it at all. `lib/deployTargets.ts` is what tells the
   * two apart; nothing should read this field and conclude "unsupported".
   */
  logs_url?: string;
  base_url?: string;
  /** Android package this env installs as; the guard for the device tools. */
  app_package?: string;
  /** Where the installable artifact (.apk) for this env lives. */
  app_url?: string;
  auto_rollback: boolean;
  created_at: string;
  updated_at: string;
}

export interface DeployConfigView {
  kind: string;
  targets: DeployTarget[] | null;
  templates: DeployTemplate[] | null;
  missing?: Record<string, string[]>;
  envs: DeployEnv[];
  /**
   * Best-effort store identity read off the repository tree (Info.plist /
   * build.gradle, etc.). Always present once the server ships it, but both
   * fields may be "" when nothing was detected — never treat presence of the
   * key as proof either field is populated. Optional here because older
   * servers omit the field entirely.
   */
  detected_app_identity?: {
    bundle_id: string;
    package_name: string;
  };
}

/**
 * PUT /v1/repositories/:id/deploy/targets is a full upsert of one
 * (repository, env) row: every field the server knows is written from this
 * payload, so an omitted field is a *cleared* field, not an untouched one.
 * Any editor that sends a subset silently wipes the rest — which is why
 * app_package/app_url/logs_url are part of this type even where no form
 * shows them.
 */
export interface SaveDeployTargetInput {
  /** See DeployTarget.sub_project_path. */
  sub_project_path?: string;
  env: DeployEnv;
  provider: DeployProvider;
  template_id?: string;
  vars?: Record<string, string>;
  health_url?: string;
  /** Ignored (not rejected) by servers older than the field. */
  logs_url?: string;
  base_url?: string;
  app_package?: string;
  app_url?: string;
  auto_rollback: boolean;
}

// Mobile store deploy: the credential vault (App Store Connect / Google
// Play console auth) plus the per-repository store app registry those
// credentials unlock.
export type StoreCredentialProvider = "asc" | "google_play";

// StoreCredentialView never carries the payload (key material / service
// account JSON) — only whether a known provider is configured and when it
// was last written. One entry per known provider, always, so "declared but
// never saved" (configured: false) is a real, renderable case.
export interface StoreCredentialView {
  provider: StoreCredentialProvider;
  configured: boolean;
  updated_at: string;
}

export type MobileStorePlatform = "ios" | "android";

export type MobileStoreState = "unregistered" | "onboarding" | "test_ready" | "live";

export interface MobileStoreChecklistItem {
  key: string;
  title: string;
  done: boolean;
  verified_at?: string;
}

export interface MobileStoreApp {
  id: string;
  app_name?: string;
  tracks?: StoreTracks;
  tracks_synced_at?: string;
  repository_id: string;
  platform: MobileStorePlatform;
  identifier: string;
  store_app_id?: string;
  state: MobileStoreState;
  review_state?: string;
  last_submitted_version?: string;
  last_released_version?: string;
  checklist?: MobileStoreChecklistItem[];
  onboarding_task_id?: string;
  first_published_at?: string;
  created_at: string;
  updated_at: string;
}

// Operations console: the cross-repository deploy matrix, run history and
// audit log (domain.DeploymentRun / deployops.MatrixView / domain.OpsAuditEntry),
// plus the store app registry projection used by the all-repos apps view.
// DeployEnv is declared above (line 137) — reused rather than redeclared.
export interface DeploymentRun {
  id: string;
  repository_id: string;
  env: DeployEnv;
  run_id: number;
  run_number: number;
  workflow_file: string;
  head_sha: string;
  head_ref: string;
  status: "queued" | "in_progress" | "completed";
  conclusion: "" | "success" | "failure" | "cancelled";
  html_url: string;
  // "local" is a break-glass deploy driven from somebody's machine while
  // GitHub Actions could not run, reported back by the record_local_deploy
  // tool. Such a run has no html_url — there is no Actions run to link to.
  trigger_source: "ui" | "rollback" | "external" | "local";
  triggered_by: string;
  rollback_of_sha: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface MatrixCell {
  env: DeployEnv;
  configured: boolean;
  dispatchable: boolean;
  provider: string;
  health_url: string;
  auto_rollback: boolean;
  last_run?: DeploymentRun;
  rollback_sha: string;
}

export interface MatrixRepo { id: string; name: string; kind: string; cells: MatrixCell[] }
export interface MatrixView { repos: MatrixRepo[] | null; envs: DeployEnv[] }

export interface OpsAuditEntry {
  id: string;
  repository_id?: string;
  action: string;
  target: string;
  actor: string;
  detail: Record<string, string>;
  outcome: "ok" | "error";
  error: string;
  created_at: string;
}

export interface StoreAppView extends MobileStoreApp { repository_name: string }

// One app as the store console lists it, for the picker that binds a
// repository to an app. Distinct from MobileStoreApp: that row is our own
// lifecycle state for something we ship, this is a remote record we neither
// own nor persist whole.
export interface StoreAppRef {
  store_app_id: string;
  identifier: string;
  name: string;
  state?: string;
}

// listing_available is stated rather than inferred from an empty list: "this
// credential cannot enumerate" and "this account has no apps" are different
// answers, and only the first one opens the manual identifier field. The Play
// Developer API has no listing endpoint at all — that list comes from the
// separate Reporting API, which a service account may not reach.
// Why the list is empty. "not_connected" means no credential has been saved
// for this provider yet — the answer is Integrations, not a retry, and the
// manual identifier field is useless because there is nothing to verify it
// against. "listing_unsupported" means the credential is fine but the store
// will not enumerate, which is exactly when typing the identifier is the way
// through. Absent while `listing_available` is true.
export type ListingReason = "not_connected" | "listing_unsupported";

export interface StoreAppListing {
  listing_available: boolean;
  reason?: ListingReason;
  apps: StoreAppRef[];
}

export type StoreChannel = "internal" | "external" | "production";

export type TrackStatus = "none" | "draft" | "in_review" | "rolling_out" | "halted" | "live";

// has_release false means the channel is empty and every other field is
// meaningless — which is why it is stated, not inferred from a blank version.
export interface TrackRelease {
  has_release: boolean;
  version?: string;
  build?: string;
  status?: TrackStatus;
  user_fraction?: number;
  audience?: string;
  updated_at?: string;
}

export interface StoreTracks {
  internal: TrackRelease;
  external: TrackRelease;
  production: TrackRelease;
}

// Where a mobile release is built and uploaded. "auto" is GitHub Actions with
// the paired Mac as the fallback; when neither can run, the release is blocked
// rather than quietly downgraded.
export type ReleaseEngine = "auto" | "github_actions" | "local";

export interface BuildStart {
  platform: MobileStorePlatform;
  engine: ReleaseEngine;
  artifacts: string[];
}

export type IncidentPolicy = "off" | "suggest" | "auto_fix";
export type IncidentSeverity = "critical" | "high" | "medium" | "low";
export type IncidentStatus = "open" | "triaging" | "proposed" | "fixing" | "resolved" | "ignored";

export interface IncidentEvent {
  id: string;
  incident_id: string;
  kind: string;
  message: string;
  created_at: string;
}

export interface Incident {
  id: string;
  repository_id: string;
  env: string;
  source: string;
  fingerprint: string;
  title: string;
  detail?: string;
  severity: IncidentSeverity;
  status: IncidentStatus;
  payload?: Record<string, unknown>;
  remedy?: string;
  remedy_kind?: string;
  confidence: number;
  occurrences: number;
  task_id?: string;
  first_seen_at: string;
  last_seen_at: string;
  resolved_at?: string;
  events?: IncidentEvent[];
}

export type RepoKind = "backend" | "frontend" | "mobile" | "worker" | "monorepo";
export type SubRepoKind = "backend" | "frontend" | "mobile" | "worker";
export type MobilePlatform = "" | "ios" | "android" | "cross_platform";
/** One sub-project inside a monorepo: where it lives and what it is. See
 * domain.RepoSubProject on the server — distinct from `sub_repo_kinds`
 * (a deduplicated set of kinds used for pipeline job routing). */
export interface RepoSubProject {
  path: string;
  kind: SubRepoKind;
  docs?: RepositoryDocs;
  mobile_platform?: MobilePlatform;
  // Per-sub-project overrides for the repo-level test gates below. Absent
  // on any of the four means "inherit the repository-level setting" — there
  // is no separate "inherit" sentinel, just field absence.
  coverage_enabled?: boolean;
  coverage_threshold?: number;
  mutation_enabled?: boolean;
  mutation_threshold?: number;
}

/** Which reference doc a repository (or sub-project) points at, for each of
 * the four kinds. "" / absent = not set. See domain.RepositoryDocs. */
export interface RepositoryDocs {
  coding_standards?: string;
  test_standards?: string;
  architecture?: string;
  local_run?: string;
}

export type RepoDocKind = "coding_standards" | "test_standards" | "architecture" | "local_run";

/** GET /v1/repositories/{id}/docs/task response. An empty `task_id` means no
 * docs-bundle task is currently active for the repository. */
export type RepoDocsTaskStatus = {
  task_id: string;
  task_key?: string;
  title?: string;
  column: string;
  pr_url?: string;
  pr_number?: number;
  merged?: boolean;
};

/** One directory entry the picker can navigate into or select. */
export interface RepositoryDirectoryEntry {
  name: string;
  /** Repo-relative, slash-separated. */
  path: string;
}

/** GET /v1/repositories/{id}/directories response — one folder level. */
export interface RepositoryDirectoryListing {
  /** The listed directory's own repo-relative path ("" = root). */
  path: string;
  /** The parent directory's path, or null when `path` is already the root. */
  parent: string | null;
  /** `path`'s own detected kind — defaults a manually-added sub-project. */
  kind: SubRepoKind;
  entries: RepositoryDirectoryEntry[];
}
export type PipelineCategory =
  | "validate"
  | "build"
  | "test"
  | "mutation_test"
  | "pr_open"
  | "stage_deploy"
  | "preprod_deploy"
  | "prod_deploy";

export interface RepositoryPipelineJob {
  id?: string;
  repository_id?: string;
  /** "" = the repository itself; distinguishes two sub-projects of the same kind. */
  sub_project_path?: string;
  sub_repo_kind: string;
  category: PipelineCategory;
  target_kind: "job" | "workflow";
  target_ref: string;
  auto_detected?: boolean;
}

export interface PipelineJobCandidate {
  ref: string;
  workflow_file?: string;
}

export interface PipelineCategorySuggestion {
  sub_project_path?: string;
  sub_repo_kind: string;
  category: PipelineCategory;
  target_kind: "job" | "workflow";
  auto: string;
  ambiguous: boolean;
  candidates: PipelineJobCandidate[];
}

export interface PipelineConfigView {
  kind: RepoKind;
  sub_repo_kinds: string[] | null;
  auto_release_on_done: boolean;
  has_workflows: boolean;
  suggestions: PipelineCategorySuggestion[];
  saved: RepositoryPipelineJob[] | null;
}

export interface SavePipelineConfigInput {
  kind: RepoKind;
  sub_repo_kinds: string[];
  auto_release_on_done: boolean;
  jobs: RepositoryPipelineJob[];
}

export type Project = Repository;

export type TaskType = "task" | "analiz" | "bug";

export type TaskPriority = "low" | "medium" | "high" | "critical";

/**
 * `blocks` is a planning statement (who may start work) and gates nothing.
 * `deploy_depends_on` is a shipping-order statement and IS enforced: the source
 * task's release is refused until the target is live in production.
 */
export type TaskRelationType = "blocks" | "deploy_depends_on";

export type CriterionReviewRole = "qa" | "pm";

// One reviewer role's own verdict on a criterion. `completed` on the
// criterion is the implementer's claim; QA and PM each verify independently.
export interface CriterionCheck {
  id: string;
  criterion_id: string;
  role: CriterionReviewRole;
  agent_id?: string;
  approved: boolean;
  note?: string;
  checked_at: string;
}

export interface AcceptanceCriterion {
  id: string;
  task_id: string;
  text: string;
  position: number;
  completed: boolean;
  // Cancelled is not completed: the criterion was dropped from scope and
  // cancel_reason says why. The card shows it struck through with the reason.
  canceled: boolean;
  cancel_reason?: string;
  created_at: string;
  checks?: CriterionCheck[];
}

// The round a task was actually given, kept alongside its criteria: every case
// derived from the request — including the ones considered and rejected as
// invalid, and the ones that could not be executed.
export type TestCaseStatus = "planned" | "passed" | "failed" | "skipped" | "invalid";

export type TestCaseCategory =
  | "happy_path"
  | "boundary"
  | "negative"
  | "auth"
  | "empty_state"
  | "regression"
  | "visual"
  | "async"
  | "other";

export interface TaskTestCase {
  id: string;
  task_id: string;
  criterion_id?: string;
  title: string;
  category: TestCaseCategory;
  status: TestCaseStatus;
  expected?: string;
  actual?: string;
  evidence?: string;
  notes?: string;
  position: number;
  created_at: string;
  updated_at: string;
}

export interface TaskTestCaseInput {
  criterion_id?: string;
  title: string;
  category?: TestCaseCategory;
  status?: TestCaseStatus;
  expected?: string;
  actual?: string;
  evidence?: string;
  notes?: string;
  position?: number;
}

export interface TestCaseSummary {
  total: number;
  passed: number;
  failed: number;
  skipped: number;
  invalid: number;
  planned: number;
}

export interface AcceptanceCriterionInput {
  text: string;
  position?: number;
  completed?: boolean;
}

export interface TaskRelation {
  id: string;
  source_task_id: string;
  target_task_id: string;
  relation_type: TaskRelationType;
  target_key?: string;
  created_at: string;
}

export interface TaskRelationInput {
  target_task_id?: string;
  target_key?: string;
  relation_type: TaskRelationType;
}

export interface TaskDocument {
  id: string;
  task_id: string;
  title: string;
  content: string;
  position: number;
  created_by_type: string;
  created_by_id: string;
  created_at: string;
  updated_at: string;
}

export interface CreateTaskDocumentInput {
  title: string;
  content?: string;
  position?: number;
}

export interface UpdateTaskDocumentInput {
  title?: string;
  content?: string;
  position?: number;
}

/**
 * Metadata of a stored binary attachment (image/document). The bytes live in
 * Postgres and are served only by GET /v1/attachments/{id} — which requires
 * the Authorization header, so images must be fetched as blobs (see
 * useAttachmentBlob), never referenced from a raw <img src>.
 */
export interface AttachmentMeta {
  id: string;
  repository_id?: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  sha256: string;
  created_by_type: string;
  created_by_id?: string;
  created_at: string;
}

export interface BoardTask {
  id: string;
  repository_id: string;
  key: string;
  task_number: number;
  title: string;
  task_type: TaskType;
  description: string;
  technical_description: string;
  initiative_project_id?: string;
  column: TaskColumn;
  position: number;
  priority: TaskPriority;
  created_by: string;
  assignee_agent_id?: string;
  created_at: string;
  updated_at: string;
  acceptance_criteria?: AcceptanceCriterion[];
  relations?: TaskRelation[];
  documents?: TaskDocument[];
  attachments?: AttachmentMeta[];
  latest_pipeline_status?: string;
  // Travels beside the status because a card whose spinner stopped has to say
  // WHY: "skipped" alone cannot tell a repo with no CI from one whose CI ran
  // out of quota while the card waited.
  latest_pipeline_gate_reason?: PipelineGateReason;
  /** Set while an agent is waiting on a human answer in blocked_session_id. */
  blocked_question?: string;
  blocked_session_id?: string;
  blocked_at?: string;
  /**
   * The shared resource the task is parked on — "claude_code_quota",
   * "mobile_device", "deploy_watch", "work_order", "human_decision". Set
   * instead of blocked_session_id: nobody answers a resource block, a sweeper
   * releases it — except "human_decision", which only a human clears.
   * blocked_question then carries the resource's detail line, not a question.
   */
  blocked_resource?: string;
  /** When the resource is expected to free up. Only timed parks have one. */
  blocked_resume_at?: string | null;
  /** When the task entered the column it is in now. Absent for tasks that predate the span ledger. */
  column_entered_at?: string;
  /** Detected from the branch diff: the task changes the database schema. */
  has_migration?: boolean;
  /** The pull request opened for this task; absent until the branch is pushed. */
  pr_url?: string | null;
  pr_number?: number | null;
  /**
   * The squash commit the task's pull request was merged as. Set only once the
   * merge actually landed, which is what makes it the card's own record of the
   * merge — the merge is no longer announced in a comment.
   */
  merge_commit_sha?: string | null;
  /** Set by the stage deploy that actually applied this task's change. */
  stage_verified_at?: string;
  /** Pre-deploy checklist; posted on the task automatically when the release is dispatched. */
  before_deploy?: string;
  /** Post-deploy steps; posted automatically when the production deploy succeeds. */
  after_deploy?: string;
  /** How to undo this change; posted alongside the pre-deploy checklist. */
  rollback_plan?: string;
}

export type ProjectTask = BoardTask;

export interface InitiativeProject {
  id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface CreateBoardTaskInput {
  title: string;
  task_type?: TaskType;
  description?: string;
  technical_description?: string;
  initiative_project_id?: string;
  column?: TaskColumn;
  priority?: TaskPriority;
  created_by?: string;
  assignee_agent_id?: string;
  acceptance_criteria?: AcceptanceCriterionInput[];
  relations?: TaskRelationInput[];
  documents?: CreateTaskDocumentInput[];
  before_deploy?: string;
  after_deploy?: string;
  rollback_plan?: string;
}

export interface UpdateBoardTaskInput {
  title?: string;
  task_type?: TaskType;
  description?: string;
  technical_description?: string;
  initiative_project_id?: string | null;
  column?: TaskColumn;
  position?: number;
  priority?: TaskPriority;
  /**
   * The assignee reads three spellings (`domain.Nullable` on the server):
   *
   *   omitted   leave whoever is on the card alone
   *   null      unassign
   *   a value   assign that agent (`""` also unassigns)
   *
   * This was NOT always true. The field was a plain Go pointer, where an
   * explicit `null` decodes to the same nil as an omitted key — so "clear the
   * assignee" was a silent no-op on the wire. Do not narrow the type back to a
   * shape that can only spell one of the three.
   */
  assignee_agent_id?: string | null;
  before_deploy?: string;
  after_deploy?: string;
  rollback_plan?: string;
  /**
   * Replaces the task's deploy_depends_on relations wholesale, leaving every
   * other relation type untouched. `[]` clears them; omitting the field leaves
   * them alone.
   */
  deploy_depends_on?: TaskRelationInput[];
}

/** Status of a release train. Only `cancelled` may be requested by a client. */
export type DeployPackageStatus = "draft" | "releasing" | "released" | "failed" | "cancelled";

export interface DeployPackageTask {
  task_id: string;
  position: number;
  key?: string;
  title?: string;
  column?: TaskColumn;
  /** Whether this member has production evidence yet. */
  released: boolean;
}

export interface DeployPackage {
  id: string;
  repository_id: string;
  name: string;
  description?: string;
  status: DeployPackageStatus;
  /** Why a package failed (which member, which error) or was cancelled. */
  note?: string;
  created_at: string;
  updated_at: string;
  tasks?: DeployPackageTask[];
}

export interface WorkspaceChunk {
  id: string;
  file_path: string;
  symbol_name: string;
  kind: string;
  start_line: number;
  end_line: number;
  language: string;
  signature: string;
  content: string;
  score?: number;
}

export interface WorkspaceIndex {
  id: string;
  project_id?: string;
  session_id?: string;
  /** Commit the index was built from; empty for a checkout without git. */
  commit_sha?: string;
  /** Why the clone could not be advanced to origin before the last pass. */
  sync_warning?: string;
  root_path: string;
  status: "pending" | "running" | "completed" | "failed";
  file_count: number;
  chunk_count: number;
  symbol_count: number;
  files_total: number;
  files_processed: number;
  indexed_at?: string;
  error?: string;
}

/** Which embedding corpus the map projects. */
export type EmbeddingMapSource = "files" | "code";

/** RAG document corpus availability, as reported by the sources endpoint. */
export interface EmbeddingMapFilesSource {
  available: boolean;
  chunk_count: number;
  document_count: number;
}

/** One indexed repository that can be projected on its own map. */
export interface EmbeddingMapRepositorySource {
  id: string;
  name: string;
  branch: string;
  index_id: string;
  chunk_count: number;
  file_count: number;
  indexed_at?: string;
}

export interface EmbeddingMapSources {
  files: EmbeddingMapFilesSource;
  repositories: EmbeddingMapRepositorySource[] | null;
}

/**
 * One chunk in the embedding map. `vector` is the PCA-reduced embedding the
 * client feeds to UMAP — not a 2D screen position.
 */
export interface EmbeddingMapPoint {
  id: string;
  group_id: string;
  group_label: string;
  chunk_index: number;
  snippet: string;
  /** Empty when the backend has no language for the chunk. */
  language?: string;
  /** Empty when the chunk is not tied to a symbol. */
  symbol?: string;
  vector: number[];
}

export interface EmbeddingMapResponse {
  source: EmbeddingMapSource;
  /** Empty string for the RAG document source. */
  repository_id: string;
  branch: string;
  dimensions: number;
  total: number;
  sampled: number;
  truncated: boolean;
  points: EmbeddingMapPoint[] | null;
}

/** One registered test phone, as the settings page sees it. */
export interface MobileDeviceStatus {
  /** Empty for the env-configured fallback: it has no row to address. */
  id: string;
  name: string;
  /** "android" is the only value the cluster can drive; see the add flow. */
  platform: string;
  /** Owned by cluster config, so the UI offers no rename and no delete. */
  managed: boolean;
  configured: boolean;
  hub_url?: string;
  device_udid?: string;
  platform_version?: string;
  device_addr?: string;
  /** Secrets are reported as booleans; the API never echoes them back. */
  has_pin: boolean;
  has_token: boolean;
  /** The hub address and its token come from cluster config, so the form omits them. */
  hub_managed: boolean;
  last_connected_at?: string;
  /** Probed live: the bridge is up, and the phone is answering it. */
  hub_reachable: boolean;
  device_online: boolean;
  /** A leased phone is healthy — kept apart from offline on purpose. */
  device_busy: boolean;
  detail?: string;
  /** "settings" when registered here, "env" when config.yml still wins. */
  source?: string;
}

/**
 * Every mutation answers with the whole list rather than the one row it
 * touched, because attaching a phone can move the fallback device out of the
 * way and pairing changes a status the server re-probes for all of them; a
 * single-row reply would leave the page rendering a list it knows is stale.
 */
export interface MobileDeviceListResponse {
  devices: MobileDeviceStatus[];
}

export interface CreateMobileDeviceRequest {
  name: string;
  /** Sent explicitly even though only "android" is selectable, so an installation that later gains a macOS host needs no client change. */
  platform: string;
  /** The phone's tailnet host:port — the Tailscale IP plus the wireless-debugging port. */
  device_addr: string;
  /** null keeps the stored credential; "" clears it. */
  device_pin?: string | null;
}

export interface UpdateMobileDeviceRequest {
  /** Omitted for the env-configured device, whose name is not ours to change. */
  name?: string;
  device_addr: string;
  platform_version?: string;
  /** Same pointer semantics as on create: omitted/null keeps, "" clears. */
  device_pin?: string | null;
}

export interface AppSettings {
  workspace_root: string;
  default_language: string;
  pipeline_container_runtime?: string;
  boilerplate_catalog_repo?: string;
  analiz_assignee_backend?: string;
  analiz_assignee_frontend?: string;
  analiz_assignee_mobile?: string;
}

export interface UpdateAnalizAssignmentRequest {
  backend?: string;
  frontend?: string;
  mobile?: string;
  confirm_grant_tools?: boolean;
}

export interface AnalizAssignmentResult {
  saved: boolean;
  settings?: AppSettings;
  missing_tools?: Record<string, string[]>;
  granted_tools?: Record<string, string[]>;
  hint?: string;
}

export interface GitHubConnectionStatus {
  connected: boolean;
  login?: string;
  detail?: string;
}

export interface VercelConnectionStatus {
  connected: boolean;
  username?: string;
  email?: string;
  /** Default scope for project lookups; "" is the personal account. */
  team_id: string;
  team_slug?: string;
  detail?: string;
}

export interface VercelTeam {
  id: string;
  slug: string;
  name: string;
}

export interface VercelGitLink {
  type: string;
  org: string;
  repo: string;
  production_branch?: string;
}

export interface VercelProject {
  id: string;
  name: string;
  framework?: string;
  root_directory?: string;
  link?: VercelGitLink;
  production_url?: string;
  team_id?: string;
  team_slug?: string;
  updated_at?: string;
}

// ---- Google Cloud -------------------------------------------------------
// The operator's own project, read-only, behind a service account they saved.

export type GCloudResourceType = "cloud_run_service" | "gke_cluster";

export interface GCloudCredentialView {
  connected: boolean;
  client_email?: string;
  project_id?: string;
  updated_at: string;
}

export interface GCloudResourceRef {
  type: GCloudResourceType;
  name: string;
  display_name: string;
  project_id: string;
  location: string;
  uri?: string;
  state?: string;
}

/** One family's verdict, in the same vocabulary as the top-level listing:
 * a credential holding only roles/run.viewer still gets a usable Cloud Run
 * picker instead of one blanket failure. */
export interface GCloudFamilyListing {
  listing_available: boolean;
  reason?: ListingReason;
  unreachable_locations?: string[];
}

/**
 * `workloads_available` is always false and saying so is the point: a shared
 * agent-server outside the customer's VPC cannot route to a private GKE
 * control plane at all, so an absent field would let the console render an
 * empty workload list as "these clusters run nothing".
 */
export interface GCloudGKEListing extends GCloudFamilyListing {
  workloads_available: boolean;
  workloads_reason?: ListingReason;
}

export interface GCloudResourceListing {
  listing_available: boolean;
  reason?: ListingReason;
  resources: GCloudResourceRef[];
  cloud_run: GCloudFamilyListing;
  gke: GCloudGKEListing;
}

export interface CloudRunTrafficTarget {
  revision: string;
  percent: number;
  tag?: string;
  uri?: string;
}

export interface CloudRunServiceDetail {
  ref: GCloudResourceRef;
  latest_ready_revision: string;
  latest_created_revision: string;
  image: string;
  traffic: CloudRunTrafficTarget[];
  ready: string;
  ready_reason?: string;
  ready_message?: string;
  update_time?: string;
}

export interface GKENodePool {
  name: string;
  status: string;
  node_count: number;
  version?: string;
  machine_type?: string;
}

export interface GKEClusterDetail {
  ref: GCloudResourceRef;
  status: string;
  status_message?: string;
  master_version?: string;
  node_count: number;
  node_pools?: GKENodePool[];
  autopilot: boolean;
  private_endpoint: boolean;
  workloads_available: boolean;
  /** Prose, not a code — it names which wall blocked this specific cluster. */
  workloads_note?: string;
}

export interface GCloudResourceDetails {
  ref: GCloudResourceRef;
  cloud_run?: CloudRunServiceDetail;
  gke_cluster?: GKEClusterDetail;
}

export interface GCloudResourceBinding {
  id: string;
  repository_id: string;
  sub_project_path: string;
  resource_type: GCloudResourceType;
  resource_name: string;
  display_name?: string;
  project_id?: string;
  location?: string;
  source: string;
  created_at: string;
  updated_at: string;
}

export interface VercelProjectListing {
  listing_available: boolean;
  reason?: ListingReason;
  projects: VercelProject[];
}

export interface VercelDeployment {
  id: string;
  state?: string;
  target?: string;
  url?: string;
  inspector_url?: string;
  created_at?: string;
  ready_at?: string;
  commit_sha?: string;
  commit_ref?: string;
  commit_message?: string;
  commit_author?: string;
  error_code?: string;
  error_message?: string;
}

/** One repository scope bound to one Vercel project. `sub_project_path` is ""
 * for the repository itself. */
export interface VercelProjectLink {
  id: string;
  repository_id: string;
  sub_project_path: string;
  project_id: string;
  project_name?: string;
  team_id: string;
  team_slug?: string;
  framework?: string;
  root_directory?: string;
  production_url?: string;
  created_at: string;
  updated_at: string;
}

/**
 * `last_failed_deployment` is reported apart from `latest_deployment` on
 * purpose: a project whose last build failed and was then fixed still wants
 * the failure visible, and a currently broken one carries the same deployment
 * in both. `warnings` is how a live read that partly failed still answers —
 * the stored link is returned rather than an error.
 */
export interface VercelProjectDetails {
  link: VercelProjectLink;
  production_url?: string;
  framework?: string;
  root_directory?: string;
  latest_deployment?: VercelDeployment;
  last_failed_deployment?: VercelDeployment;
  warnings?: string[];
}

/** "" is the whole repository; a monorepo uses its sub-repo kinds. */
export type HostingArea = "" | SubRepoKind;
export type HostingLinkSource = "detected" | "user";
export type HostingConfidence = "exact" | "ambiguous" | "none";
export type HostingMatchReason = "project_json" | "git_link_dir" | "git_link" | "name";

export interface HostingLink {
  id: string;
  repository_id: string;
  area: HostingArea;
  provider: DeployProvider;
  external_id?: string;
  external_name?: string;
  scope_id?: string;
  scope_slug?: string;
  root_directory?: string;
  production_url?: string;
  source: HostingLinkSource;
  evidence?: string;
  created_at: string;
  updated_at: string;
}

export interface SaveHostingLinkInput {
  area: HostingArea | "root";
  provider: DeployProvider;
  external_id?: string;
  /** Omit for the connection's default team; "" for the personal account. */
  scope_id?: string;
  source?: HostingLinkSource;
  evidence?: string;
}

export interface HostingHint {
  provider: string;
  name: string;
  detail?: string;
  evidence?: string;
}

export interface HostingCandidate {
  provider: DeployProvider;
  project: VercelProject;
  reason: HostingMatchReason;
}

export interface HostingAreaDetection {
  area: HostingArea;
  kind: string;
  directory?: string;
  existing?: HostingLink;
  hints?: HostingHint[] | null;
  candidates?: HostingCandidate[] | null;
  confidence: HostingConfidence;
}

export interface HostingDetection {
  repository_id: string;
  kind: string;
  vercel_connected: boolean;
  areas: HostingAreaDetection[] | null;
  warnings?: string[] | null;
}

/** What a repository's dependency record points at: another repo's
 * sub-project, a whole other repo, or a manually-recorded database. See
 * domain.RepoDependency on the server. */
export type DependencyTargetKind = "sub_repo" | "repo" | "database";
export type DatabaseEngine = "postgres" | "mysql" | "mongodb" | "redis" | "other";

export interface RepoDependency {
  id: string;
  repository_id: string;
  target_kind: DependencyTargetKind;
  target_repository_id?: string;
  target_sub_project_path?: string;
  database_label?: string;
  database_engine?: DatabaseEngine | "";
  /** "stage" | "prod" only — see domain.ValidateRepoDependencyRequest. */
  database_env?: DeployEnv | "";
  database_host?: string;
  database_port?: number;
  database_name?: string;
  database_username?: string;
  /** Masked ("***") whenever a secret is stored, "" when none was ever set.
   * The real value never round-trips. */
  database_secret?: string;
  note?: string;
  created_at: string;
  updated_at: string;
}

export interface SaveRepoDependencyRequest {
  target_kind: DependencyTargetKind;
  target_repository_id?: string;
  target_sub_project_path?: string;
  database_label?: string;
  database_engine?: DatabaseEngine | "";
  database_env?: DeployEnv | "";
  database_host?: string;
  database_port?: number;
  database_name?: string;
  database_username?: string;
  /** "" or the masked placeholder leaves the stored secret untouched. */
  database_secret?: string;
  note?: string;
}

export interface ProjectDependencyView {
  outgoing: RepoDependency[];
  incoming: RepoDependency[];
}

export interface GitHubOwner {
  login: string;
  type: "user" | "org";
}

export interface GitHubRepoInfo {
  name: string;
  full_name: string;
  clone_url: string;
  default_branch: string;
  private: boolean;
  description: string;
}

// "claude_code", "cursor_agent", "antigravity" and "opencode" are not HTTP
// endpoints: a task on an agent with one of these providers is handed to a
// headless CLI session on this host, which carries its own subscription auth.
// They are therefore selectable on an AGENT only — the LLM settings page
// (connect / test / activate / embed) rejects them server-side, because there
// is no base URL and no key to reach.
export type LLMProviderType =
  | "local"
  | "openai"
  | "groq"
  | "gemini"
  | "anthropic"
  | "claude_code"
  | "cursor_agent"
  | "antigravity"
  // The OpenCode CLI, run the same way as the other three host-executed ones.
  | "opencode";

export interface LLMProviderDefinition {
  type: LLMProviderType;
  label: string;
  description: string;
  default_base_url: string;
  default_model: string;
  requires_api_key: boolean;
  base_url_required: boolean;
  model_required: boolean;
  default_timeout_seconds: number;
  /**
   * The provider is a PROCESS on the runner host rather than an endpoint on the
   * network. Optional because an older server omits it; treat a missing value
   * as false and fall back to {@link HOST_EXECUTED_PROVIDERS}.
   */
  host_executed?: boolean;
  /**
   * The provider can actually run work today. False means it is DECLARED but
   * not yet built — listed so the UI can show what is coming, and refused by
   * the server anywhere it would be connected, activated or put on an agent.
   *
   * Optional because a server that predates the flag omits it; treat a missing
   * value as available, since every provider such a server knows about is.
   */
  available?: boolean;
}

/**
 * A local agent CLI, named by the on-disk catalog layout it reads rather than
 * by its provider. See the server's domain.AgentCLIFlavor for why the two are
 * kept as separate words.
 */
export type AgentCLIFlavor = "claude" | "cursor" | "antigravity" | "opencode";

/**
 * One connected local agent CLI.
 *
 * Connecting is not the same act as connecting an HTTP provider, which is why
 * it has its own endpoints: there is no base URL and no key, and what "connect"
 * does is verify the binary on the runner host holds a session and write every
 * enabled agent's role, rules and skills to disk in the layout that binary
 * discovers on its own.
 *
 * Several flavors can be connected at once, one row each.
 */
export interface AgentCLIConnection {
  flavor: AgentCLIFlavor;
  provider_type: LLMProviderType;
  /** Evidence of what was verified, not configuration the client acts on. */
  binary_path: string;
  binary_version: string;
  /**
   * Where the connect-time catalog snapshot was written. NOT what a board run
   * reads — a run materialises its own agent from the database at dispatch —
   * so it is shown for inspection and never treated as the live catalog.
   */
  catalog_path: string;
  agent_count: number;
  skill_count: number;
  connected_at: string;
}

export interface AgentCLIFlavorView {
  flavor: AgentCLIFlavor;
  provider_type: LLMProviderType;
  label: string;
  available: boolean;
  connected: boolean;
}

export interface AgentCLIState {
  /**
   * Empty when nothing is connected, which is a normal state and not an
   * error. May hold more than one entry — connecting a flavor no longer
   * disconnects any other.
   */
  connections: AgentCLIConnection[];
  flavors: AgentCLIFlavorView[];
}

/**
 * The failure kinds `POST /v1/agent-cli/:flavor/connect` distinguishes, carried
 * in ApiError.type.
 *
 * They are separate because their fixes are: a CLI that was never installed
 * sends the user to an installer, one that is installed and signed out sends
 * them to a login, and a provider with no executor cannot be fixed at all. A
 * single "connect failed" message is the wrong advice for two of the three.
 */
export type AgentCLIErrorKind =
  | "agent_cli_binary_missing"
  | "agent_cli_unauthenticated"
  | "provider_unavailable";

export interface LLMProviderConfig {
  provider_type: LLMProviderType;
  base_url: string;
  default_model: string;
  timeout_seconds: number;
  configured: boolean;
  has_api_key: boolean;
  updated_at: string;
}

export interface LLMProviderView {
  definition: LLMProviderDefinition;
  config: LLMProviderConfig;
  active: boolean;
}

export interface LLMEndpoint {
  id: string;
  name: string;
  base_url: string;
  default_model: string;
  timeout_seconds: number;
  configured: boolean;
  has_api_key: boolean;
  created_at: string;
  updated_at: string;
}

export interface SaveLLMEndpointRequest {
  name: string;
  base_url: string;
  default_model: string;
  api_key: string;
  timeout_seconds: number;
}

export interface LLMProvidersResponse {
  active_provider: LLMProviderType | string;
  providers: LLMProviderView[];
  endpoints: LLMEndpoint[];
}

export interface LLMProviderHealthItem {
  provider_type: LLMProviderType;
  label: string;
  configured: boolean;
  active: boolean;
  status: "ok" | "error" | "disconnected";
  message?: string;
}

export interface HealthResponse {
  status: string;
  llm: string;
  providers?: LLMProviderHealthItem[];
}

// Model alanı yoktur: sağlayıcı bağlanırken model sorulmaz. Sunucu kayıtlı
// değeri korur, ilk kurulumda tanımın varsayılanını kullanır. Model seçimi
// ajan bazında yapılır.
export interface ConnectLLMProviderRequest {
  base_url: string;
  api_key: string;
  timeout_seconds: number;
}

export interface SessionMessage {
  id: string;
  role: string;
  content: string;
  created_at: string;
  clarification?: ClarificationRequest;
  attachments?: AttachmentMeta[];
}

export type SessionActionEntity =
  | "board_task"
  | "task_comment"
  | "task_document"
  | "acceptance_criterion"
  | "initiative_project"
  | "repository";

/**
 * One board record an agent created or changed inside a chat. Persisted so the
 * conversation keeps its history of actions — the agent loop's tool trace does
 * not outlive a single turn.
 */
export interface SessionAction {
  id: string;
  session_id: string;
  run_id?: string;
  agent_id?: string;
  tool_name: string;
  verb: string;
  entity_kind: SessionActionEntity;
  entity_id?: string;
  entity_key?: string;
  title?: string;
  column?: string;
  priority?: string;
  repository_id?: string;
  is_error: boolean;
  created_at: string;
}

export interface SessionRun {
  id: string;
  session_id?: string;
  request_id: string;
  status: string;
  model?: string;
  started_at: string;
  completed_at?: string;
}

export interface SessionStep {
  id: string;
  run_id: string;
  step_type: string;
  /** Backend may serialize this as JSON null for payload-less steps. */
  payload: Record<string, unknown> | null;
  created_at: string;
}

export interface FileRecord {
  id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
}

export interface ClarificationOption {
  id: string;
  label: string;
}

export interface ClarificationQuestion {
  id: string;
  prompt: string;
  allow_multiple?: boolean;
  options: ClarificationOption[];
}

export interface ClarificationRequest {
  context?: string;
  questions: ClarificationQuestion[];
}

export interface AgentResponse {
  message: { role: string; content: string };
  usage?: { prompt_tokens: number; completion_tokens: number; total_tokens: number };
  clarification?: ClarificationRequest;
}

export interface LLMModel {
  id: string;
  object: string;
  /**
   * Display name for the row, when the id alone does not read as one.
   *
   * Only the host-executed claude_code catalog sends it, and there it carries
   * the whole meaning of one entry: the "let the CLI choose its own model" row
   * has an EMPTY id (nothing is passed to `claude --model`) and is nothing but
   * its label. Absent for every HTTP provider — fall back to the id.
   */
  label?: string;
}

export interface Skill {
  id: string;
  agent_id: string;
  name: string;
  description: string;
  category: string;
  tags: string[];
  content: string;
  enabled: boolean;
  /** null = a general skill, not part of any tech stack. */
  tech_stack_id: string | null;
  created_at: string;
}

export interface TechStack {
  id: string;
  agent_id: string;
  name: string;
  description: string;
  position: number;
  created_at: string;
}

export interface Agent {
  id: string;
  name: string;
  description: string;
  subagent_type: string;
  system_prompt: string;
  provider_type: LLMProviderType | "";
  model: string;
  model_heavy: string;
  tool_policy: ToolPolicy;
  skill_ids: string[];
  enabled: boolean;
  self_evolution_enabled: boolean;
  created_at: string;
}

export interface OrchestratorRule {
  id: string;
  agent_id: string;
  name: string;
  content: string;
  priority: number;
  enabled: boolean;
  created_at: string;
}

export interface AgentTemplate {
  id: string;
  name: string;
  description: string;
  subagent_type: string;
  system_prompt: string;
  provider_type: LLMProviderType | "";
  model: string;
  tool_policy: ToolPolicy;
  skills: { name: string; description: string; category: string; content: string; enabled: boolean }[];
  rules: { name: string; content: string; priority: number; enabled: boolean }[];
  kpis: CreateKPIInput[];
  self_evolution_enabled: boolean;
  built_in: boolean;
  created_at: string;
  updated_at: string;
}

export interface AgentKPI {
  id: string;
  agent_id: string;
  metric_key: string;
  name: string;
  description: string;
  period: "daily" | "weekly" | "monthly";
  target_full: number;
  target_half: number;
  weight: number;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export type CreateKPIInput = Omit<AgentKPI, "id" | "agent_id" | "created_at" | "updated_at">;

export interface KPIMetricInfo {
  key: string;
  label: string;
  description: string;
  unit: string;
  direction: "lower_better" | "higher_better";
}

export interface AgentKPIResult {
  id: string;
  kpi_id: string;
  agent_id: string;
  period_start: string;
  period_end: string;
  measured_value: number;
  attainment: number;
  computed_at: string;
}

export interface AgentPerformanceScore {
  id: string;
  agent_id: string;
  score: number;
  runs_total: number;
  runs_passed: number;
  runs_revised: number;
  updated_at: string;
}

export interface AgentScoreEvent {
  id: string;
  agent_id: string;
  task_id?: string;
  event_type: string;
  delta: number;
  score_after: number;
  reason?: string;
  created_at: string;
}

export interface AgentPerformance {
  score?: AgentPerformanceScore;
  events?: AgentScoreEvent[];
  kpis?: AgentKPI[];
  kpi_results?: AgentKPIResult[];
  kpi_composite?: number;
}

/** Which of the four memory buckets a record sits in. */
export type MemoryScope = "agent_global" | "agent_project" | "team_global" | "team_project";

export interface AgentMemory {
  id: string;
  agent_id: string;
  /** Set when the memory only applies inside that repository. */
  repository_id?: string;
  content: string;
  category: string;
  scope: MemoryScope;
  source: string;
  created_at: string;
  updated_at: string;
}

export interface MemoryInput {
  content: string;
  category?: string;
  /** Omit for a global memory, set to bind the memory to one repository. */
  repository_id?: string;
}

/** Repository dimension of a memory listing. */
export type MemoryRepoScope = "any" | "project" | "global" | "visible";

export interface MemoryScopeFilter {
  repositoryId?: string;
  repoScope?: MemoryRepoScope;
}

/** One team memory that was turned into a skill on the listed agents. */
export interface MemoryPromotion {
  memory_id: string;
  skill_name: string;
  agents: string[];
}

export interface MemoryPromotionResult {
  scanned: number;
  promoted: MemoryPromotion[];
}

/** One proposed memory→skill move, shown for approval before anything is written. */
export interface MemoryPromotionCandidate {
  memory_id: string;
  memory_content: string;
  skill_name: string;
  description: string;
  category: string;
  content: string;
  agents: string[];
}

/** What a promotion WOULD do. Producing it writes nothing. */
export interface MemoryPromotionPlan {
  scanned: number;
  candidates: MemoryPromotionCandidate[];
  agents: string[];
}

function memoryScopeParams(filter: MemoryScopeFilter): URLSearchParams {
  const params = new URLSearchParams();
  if (filter.repositoryId) params.set("repository_id", filter.repositoryId);
  if (filter.repoScope) params.set("repo_scope", filter.repoScope);
  return params;
}

export interface EvolutionEvent {
  id: string;
  reflection_id?: string;
  agent_id: string;
  change_type: string;
  target_kind: string;
  target_id?: string;
  target_name: string;
  before?: unknown;
  after?: unknown;
  reverted_event_id?: string;
  score_at_change: number;
  impact: "pending" | "effective" | "regressed" | "neutral" | "insufficient_data";
  impact_evaluated_at?: string;
  created_at: string;
}

export interface AgentReflection {
  id: string;
  agent_id: string;
  trigger: string;
  status: "running" | "completed" | "failed";
  window_start: string;
  window_end: string;
  summary: string;
  performance_snapshot?: { score: number; kpi_composite: number; kpis?: Record<string, number>; captured_at: string };
  error?: string;
  created_at: string;
  completed_at?: string;
}

export interface VerificationResult {
  passed: boolean;
  issues: string[];
  summary: string;
}

export interface PlanTask {
  id: string;
  task_key: string;
  title: string;
  description: string;
  agent_id: string;
  skill_ids: string[];
  tool_names?: string[];
  depends_on: string[];
  status: string;
  result?: string;
  error?: string;
}

export interface OrchestrationPlan {
  id: string;
  run_id: string;
  status: string;
  summary: string;
  purpose?: string;
  goal?: string;
  verification?: VerificationResult;
  tasks: PlanTask[];
}

export type SkillInput = Omit<Skill, "id" | "created_at" | "agent_id">;
export type TechStackInput = Omit<TechStack, "id" | "created_at" | "agent_id">;
export type TechStackPatch = Partial<TechStackInput>;
export type AgentInput = Omit<Agent, "id" | "created_at" | "skill_ids">;
export type OrchestratorRuleInput = Omit<OrchestratorRule, "id" | "created_at" | "agent_id">;

export const MASKED_SECRET_VALUE = "••••••";

export interface MCPEnvSchemaField {
  key: string;
  label?: string;
  secret?: boolean;
  placeholder?: string;
}

export interface MCPServer {
  id: string;
  enabled: boolean;
  transport: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
  allowed_tools?: string[];
  created_at: string;
}

export interface MCPServerView extends MCPServer {
  connected: boolean;
  tool_count: number;
  tools?: string[];
  status: "disabled" | "connected" | "error";
  last_error?: string;
  env_schema?: MCPEnvSchemaField[];
  secret_fields?: string[];
}

export type MCPServerCreateInput = Omit<MCPServer, "created_at">;

export interface MCPServerUpdateInput {
  enabled: boolean;
  transport: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
  allowed_tools?: string[];
  secrets?: Record<string, string>;
}

export type MCPServerInput = MCPServerCreateInput;

const ORCHESTRATE_STORAGE = "bridge_orchestrate";
const LOCALE_STORAGE = "bridge_locale";

export function getStoredLocale(): string {
  return localStorage.getItem(LOCALE_STORAGE) ?? "en";
}

export function setStoredLocale(value: string) {
  localStorage.setItem(LOCALE_STORAGE, value);
}

/**
 * Auth headers for a call to the local server. Exported so non-JSON endpoints
 * authenticate the same way as `request` instead of rolling their own.
 */
export function authHeaders(extra: Record<string, string> = {}): Record<string, string> {
  const token = getApiToken();
  if (token) {
    return { ...extra, Authorization: `Bearer ${token}` };
  }
  return extra;
}

export function getStoredOrchestrate(): boolean {
  return localStorage.getItem(ORCHESTRATE_STORAGE) === "true";
}

export function setStoredOrchestrate(value: boolean) {
  localStorage.setItem(ORCHESTRATE_STORAGE, String(value));
}



// How long one attempt may hang before it is given up on. A request with no
// deadline is the difference between "slow" and "broken": a proxy that accepts
// the connection and then never answers left every page of this app on its
// skeleton forever, with no error and nothing to retry. Reads are cheap to
// repeat so they are cut short quickly; a write is given far longer because
// abandoning it says nothing about whether the server applied it.
const READ_TIMEOUT_MS = 20000;
// Generous, because the slowest legitimate writes here are minutes-scale: an
// embedding reindex over every skill, a provider connection test that has to
// reach the model. The number is a liveness check, not a latency budget.
const WRITE_TIMEOUT_MS = 180000;

// TimeoutError, not AbortError: isAbortError (lib/errors) means "we cancelled
// this on purpose, stay quiet", and a deadline the user never asked for must
// surface as a real failure.
function isTimeoutError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    (error as { name?: unknown }).name === "TimeoutError"
  );
}

// One deadline per attempt, combined with whatever signal the caller already
// owns so an explicit abort still wins.
function withTimeout(init: RequestInit, timeoutMs: number): RequestInit {
  if (!timeoutMs || typeof AbortSignal === "undefined" || !AbortSignal.timeout) {
    return init;
  }
  const deadline = AbortSignal.timeout(timeoutMs);
  const signal =
    init.signal && AbortSignal.any ? AbortSignal.any([init.signal, deadline]) : deadline;
  return { ...init, signal };
}

function fetchWithTimeout(url: string, init: RequestInit, timeoutMs = 0): Promise<Response> {
  return fetch(url, withTimeout(init, timeoutMs));
}

// ApiError carries the backend's machine-readable `error.type` alongside the
// message, so a caller can tell apart causes that share a status code — an
// exhausted provider quota and a rejected API key both arrive as a 400 here.
// It stays an Error subclass, so `e instanceof Error ? e.message : …` callers
// keep working untouched.
export class ApiError extends Error {
  readonly status: number;
  readonly type: string;

  constructor(message: string, status: number, type = "") {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.type = type;
  }
}

// Every non-2xx carries a JSON error body: {"error": {"message": "...", "type": "..."}}.
// The type is the machine-readable half — callers switch on it rather than on
// prose, so a reworded message is not a client-visible change.
async function apiErrorFrom(res: Response): Promise<ApiError> {
  const text = await res.text();
  let message = text;
  let type = "";
  try {
    const parsed = JSON.parse(text) as { error?: string | { message?: string; type?: string } };
    if (typeof parsed.error === "string") {
      message = parsed.error;
    } else {
      message = parsed.error?.message ?? text;
      type = parsed.error?.type ?? "";
    }
  } catch {
    /* keep text */
  }
  return new ApiError(message || `HTTP ${res.status}`, res.status, type);
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    ...(init.headers as Record<string, string>),
  };
  if (init.body && !headers["Content-Type"]) {
    headers["Content-Type"] = "application/json";
  }
  const method = (init.method ?? "GET").toUpperCase();
  const timeoutMs = method === "GET" || method === "HEAD" ? READ_TIMEOUT_MS : WRITE_TIMEOUT_MS;
  let res: Response;
  try {
    res = await fetchWithTimeout(apiUrl(path), { ...init, headers: authHeaders(headers) }, timeoutMs);
  } catch (e) {
    if (isTimeoutError(e)) {
      // 408 so callers can recognise it; the message is what the toast shows.
      throw new ApiError("The server did not answer in time. Please try again.", 408, "timeout");
    }
    throw e;
  }
  if (!res.ok) {
    throw await apiErrorFrom(res);
  }
  if (res.status === 204 || res.status === 205) {
    return undefined as T;
  }
  // Bazı uçlar gövdesiz/gövdesi düz metin döner (ör. 202 "Accepted").
  // Boş ya da JSON olmayan gövdeyi JSON.parse'a sokup patlamayalım.
  const body = await res.text();
  if (!body) {
    return undefined as T;
  }
  const contentType = res.headers.get("content-type") ?? "";
  if (!contentType.includes("json")) {
    return undefined as T;
  }
  return JSON.parse(body) as T;
}

export interface UsageTotals {
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
}

export interface UsageSummary {
  days: number;
  total: UsageTotals;
  by_model: ({ model: string } & UsageTotals)[] | null;
  daily: ({ day: string } & UsageTotals)[] | null;
}

export interface BillingStatus {
  plan_name: string;
  unlimited: boolean;
  usd_budget: number;
  usd_spent: number;
  token_budget: number;
  token_budget_used: number;
  token_remaining: number;
  raw_tokens_used: number;
  exhausted: boolean;
  period_start: string;
  reset_at: string;
  /** The server declares this plan read-only; the edit controls stay hidden. */
  managed?: boolean;
}

export interface BillingPlan {
  name: string;
  usd_budget: number;
  period_days: number;
  period_start: string;
  display_token_rate: number;
  updated_at: string;
}

export interface ModelPrice {
  model: string;
  usd_per_1m_prompt: number;
  usd_per_1m_completion: number;
  updated_at?: string;
}

// @-mention the user picked from the composer autocomplete: exact entity
// reference sent alongside the human-readable message text.
export type MessageMention = {
  kind: "agent" | "project" | "repository";
  id: string;
  name: string;
};

/**
 * URL of an attachment's raw bytes. NOTE: the endpoint requires the
 * Authorization header, which <img src> / plain anchors cannot send — use
 * api.fetchAttachmentBlob (or useAttachmentBlob) and an object URL instead.
 * Exported for the download path, which re-fetches through authHeaders anyway.
 */
export function attachmentUrl(id: string): string {
  return apiUrl(`/v1/attachments/${id}`);
}

export const api = {
  health: () => request<HealthResponse>("/health"),
  usageSummary: (days = 30) => request<UsageSummary>(`/v1/usage?days=${days}`),
  billingStatus: () => request<BillingStatus>("/v1/billing"),
  getBillingPlan: () => request<BillingPlan>("/admin/billing/plan"),
  updateBillingPlan: (data: Partial<Omit<BillingPlan, "period_start" | "updated_at">>) =>
    request<BillingPlan>("/admin/billing/plan", { method: "PUT", body: JSON.stringify(data) }),
  listModelPrices: () => request<{ prices: ModelPrice[] }>("/admin/billing/model-prices"),
  upsertModelPrice: (data: ModelPrice) =>
    request<ModelPrice>("/admin/billing/model-prices", { method: "PUT", body: JSON.stringify(data) }),
  deleteModelPrice: (model: string) =>
    request<void>(`/admin/billing/model-prices/${encodeURIComponent(model)}`, { method: "DELETE" }),

  listModels: (provider?: LLMProviderType) =>
    request<{ object: string; data: LLMModel[] }>(
      provider ? `/v1/models?provider=${encodeURIComponent(provider)}` : "/v1/models",
    ),

  getSettings: () => request<AppSettings>("/v1/settings"),
  mobileDevices: () => request<MobileDeviceListResponse>("/v1/settings/mobile-devices"),
  createMobileDevice: (body: CreateMobileDeviceRequest) =>
    request<MobileDeviceListResponse>("/v1/settings/mobile-devices", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  // The id is path-encoded on every per-device call: it is server-minted, but
  // the env-configured device carries "" and must never silently address the
  // collection route instead — callers filter it out before getting here.
  updateMobileDevice: (id: string, body: UpdateMobileDeviceRequest) =>
    request<MobileDeviceListResponse>(`/v1/settings/mobile-devices/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  deleteMobileDevice: (id: string) =>
    request<MobileDeviceListResponse>(`/v1/settings/mobile-devices/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  pairMobileDevice: (id: string, body: { pair_addr: string; code: string }) =>
    request<MobileDeviceListResponse>(`/v1/settings/mobile-devices/${encodeURIComponent(id)}/pair`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  connectMobileDevice: (id: string) =>
    request<MobileDeviceListResponse>(`/v1/settings/mobile-devices/${encodeURIComponent(id)}/connect`, {
      method: "POST",
    }),

  getGCloudCredential: () => request<GCloudCredentialView>("/v1/gcloud/credential"),

  saveGCloudCredential: (serviceAccountJson: string) =>
    request<GCloudCredentialView>("/v1/gcloud/credential", {
      method: "PUT",
      body: JSON.stringify({ service_account_json: serviceAccountJson }),
    }),

  deleteGCloudCredential: () => request<void>("/v1/gcloud/credential", { method: "DELETE" }),

  listGCloudResources: () => request<GCloudResourceListing>("/v1/gcloud/resources"),

  bindGCloudResource: (
    id: string,
    resource: { resource_type: GCloudResourceType; resource_name: string; sub_project_path?: string },
  ) =>
    request<GCloudResourceBinding>(`/v1/repositories/${id}/gcloud/resource`, {
      method: "PUT",
      body: JSON.stringify({ sub_project_path: "", ...resource }),
    }),

  gcloudResourceDetails: (id: string, subProjectPath = "") =>
    request<GCloudResourceDetails>(
      `/v1/repositories/${id}/gcloud/resource?sub_project_path=${encodeURIComponent(subProjectPath)}`,
    ),

  listVercelProjects: () => request<VercelProjectListing>("/v1/vercel/projects"),

  linkVercelProject: (id: string, projectId: string, subProjectPath = "") =>
    request<VercelProjectLink>(`/v1/repositories/${id}/vercel/project`, {
      method: "PUT",
      body: JSON.stringify({ project_id: projectId, sub_project_path: subProjectPath }),
    }),

  vercelProjectDetails: (id: string, subProjectPath = "") =>
    request<VercelProjectDetails>(
      `/v1/repositories/${id}/vercel/project?sub_project_path=${encodeURIComponent(subProjectPath)}`,
    ),

  unlinkVercelProject: (id: string, subProjectPath = "") =>
    request<void>(
      `/v1/repositories/${id}/vercel/project?sub_project_path=${encodeURIComponent(subProjectPath)}`,
      { method: "DELETE" },
    ),

  githubStatus: () =>
    request<GitHubConnectionStatus>("/v1/settings/github"),

  /** Verifies the token against GitHub before storing it; a bad one is a 400. */
  setGitHubToken: (token: string) =>
    request<GitHubConnectionStatus>("/v1/settings/github", {
      method: "PUT",
      body: JSON.stringify({ token }),
    }),

  disconnectGitHub: () =>
    request<GitHubConnectionStatus>("/v1/settings/github", { method: "DELETE" }),

  // Vercel is a pasted access token (vercel.com/account/tokens), verified and
  // stored encrypted server-side — there is no OAuth hop to a gateway here.
  vercelStatus: () => request<VercelConnectionStatus>("/v1/settings/vercel"),

  connectVercel: (data: { token?: string; team_id?: string }) =>
    request<VercelConnectionStatus>("/v1/settings/vercel", {
      method: "PUT",
      body: JSON.stringify(data),
    }),

  disconnectVercel: () =>
    request<VercelConnectionStatus>("/v1/settings/vercel", { method: "DELETE" }),

  vercelTeams: () => request<{ teams: VercelTeam[] }>("/v1/settings/vercel/teams"),

  // teamId undefined → the connection's default scope; "" → personal account.
  vercelProjects: (teamId?: string) =>
    request<{ projects: VercelProject[]; count: number }>(
      teamId === undefined
        ? "/v1/settings/vercel/projects"
        : `/v1/settings/vercel/projects?team_id=${encodeURIComponent(teamId)}`,
    ),

  detectHosting: (repositoryId: string) =>
    request<HostingDetection>(`/v1/repositories/${repositoryId}/hosting/detect`),

  listHostingLinks: (repositoryId: string) =>
    request<{ links: HostingLink[] }>(`/v1/repositories/${repositoryId}/hosting/links`),

  saveHostingLink: (repositoryId: string, data: SaveHostingLinkInput) =>
    request<HostingLink>(`/v1/repositories/${repositoryId}/hosting/links`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),

  // "root" addresses the whole-repository area, which cannot travel as "".
  deleteHostingLink: (repositoryId: string, area: HostingArea) =>
    request<void>(`/v1/repositories/${repositoryId}/hosting/links/${area || "root"}`, {
      method: "DELETE",
    }),

  listRepoDependencies: (repositoryId: string) =>
    request<{ dependencies: RepoDependency[] }>(`/v1/repositories/${repositoryId}/dependencies`),

  createRepoDependency: (repositoryId: string, data: SaveRepoDependencyRequest) =>
    request<RepoDependency>(`/v1/repositories/${repositoryId}/dependencies`, {
      method: "POST",
      body: JSON.stringify(data),
    }),

  updateRepoDependency: (repositoryId: string, depId: string, data: SaveRepoDependencyRequest) =>
    request<RepoDependency>(`/v1/repositories/${repositoryId}/dependencies/${depId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),

  deleteRepoDependency: (repositoryId: string, depId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/dependencies/${depId}`, {
      method: "DELETE",
    }),

  listProjectDependencies: (projectId: string) =>
    request<ProjectDependencyView>(`/v1/projects/${projectId}/dependencies`),

  githubOwners: () => request<{ owners: GitHubOwner[] }>("/v1/settings/github/owners"),

  githubOwnerRepos: (owner: string) =>
    request<{ repos: GitHubRepoInfo[] }>(
      `/v1/settings/github/repos?owner=${encodeURIComponent(owner)}`,
    ),

  importGitHubRepository: (data: {
    owner: string;
    name: string;
    clone_url?: string;
    description?: string;
    project_ids?: string[];
  }) =>
    request<Repository>("/v1/repositories/import", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  updateSettings: (settings: Partial<AppSettings>) =>
    request<AppSettings>("/v1/settings", {
      method: "PUT",
      body: JSON.stringify(settings),
    }),

  // 422 with a missing_tools body is a normal outcome here (the caller opens a
  // confirmation dialog), not an error — so this bypasses request()'s
  // throw-on-non-2xx and returns the parsed AnalizAssignmentResult either way.
  updateAnalizAssignment: async (req: UpdateAnalizAssignmentRequest) => {
    const res = await fetchWithTimeout(
      apiUrl("/v1/settings/analiz-assignment"),
      {
        method: "PUT",
        headers: authHeaders({ "Content-Type": "application/json" }),
        body: JSON.stringify(req),
      },
      WRITE_TIMEOUT_MS,
    );
    if (!res.ok && res.status !== 422) {
      throw await apiErrorFrom(res);
    }
    return (await res.json()) as AnalizAssignmentResult;
  },

  listLLMProviders: () => request<LLMProvidersResponse>("/v1/llm/providers"),

  connectLLMProvider: (type: LLMProviderType, body: ConnectLLMProviderRequest) =>
    request<LLMProvidersResponse>(`/v1/llm/providers/${type}/connect`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  activateLLMProvider: (type: LLMProviderType) =>
    request<LLMProvidersResponse>(`/v1/llm/providers/${type}/activate`, {
      method: "POST",
    }),

  testLLMProvider: (type: LLMProviderType, body: ConnectLLMProviderRequest) =>
    request<{ ok: boolean }>(`/v1/llm/providers/${type}/test`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  disconnectLLMProvider: (type: LLMProviderType) =>
    request<LLMProvidersResponse>(`/v1/llm/providers/${type}`, {
      method: "DELETE",
    }),

  // Local agent CLIs. A separate surface from /v1/llm/providers on purpose:
  // those routes all mean "dial this base URL with this key" and the server
  // refuses host-executed providers on every one of them.
  getAgentCLIState: () => request<AgentCLIState>("/v1/agent-cli"),

  connectAgentCLI: (flavor: AgentCLIFlavor) =>
    request<AgentCLIState>(`/v1/agent-cli/${flavor}/connect`, { method: "POST" }),

  disconnectAgentCLI: (flavor: AgentCLIFlavor) =>
    request<AgentCLIState>(`/v1/agent-cli/${flavor}`, { method: "DELETE" }),

  createLLMEndpoint: (body: SaveLLMEndpointRequest) =>
    request<LLMProvidersResponse>("/v1/llm/endpoints", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateLLMEndpoint: (id: string, body: SaveLLMEndpointRequest) =>
    request<LLMProvidersResponse>(`/v1/llm/endpoints/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  deleteLLMEndpoint: (id: string) =>
    request<LLMProvidersResponse>(`/v1/llm/endpoints/${id}`, {
      method: "DELETE",
    }),

  testLLMEndpoint: (id: string, body: SaveLLMEndpointRequest) =>
    request<{ ok: boolean }>(`/v1/llm/endpoints/${id || "new"}/test`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  activateLLMEndpoint: (id: string) =>
    request<LLMProvidersResponse>(`/v1/llm/endpoints/${id}/activate`, {
      method: "POST",
    }),

  listSessions: (projectId?: string) =>
    request<{ sessions: Session[] }>(
      projectId ? `/v1/sessions?project_id=${encodeURIComponent(projectId)}` : "/v1/sessions",
    ),

  listAgentSessions: (agentId: string) =>
    request<{ sessions: Session[] }>(
      `/v1/sessions?agent_id=${encodeURIComponent(agentId)}`,
    ),

  createSession: (data: {
    title: string;
    model?: string;
    project_id?: string;
    agent_id?: string;
  }) =>
    request<Session>("/v1/sessions", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  getSession: (id: string) =>
    request<{ session: Session; messages: SessionMessage[]; actions?: SessionAction[] }>(
      `/v1/sessions/${id}`,
    ),

  deleteSession: (id: string) =>
    request<void>(`/v1/sessions/${id}`, { method: "DELETE" }),

  /**
   * Stops the agent turn this session has in flight. `cancelled: false` means
   * there was nothing left to stop — the answer landed a moment before the
   * button was pressed — which is an ordinary outcome, not an error.
   *
   * Aborting the SSE request alone is not enough: that only closes the reader,
   * and the run keeps its row and finishes on the server. This is the half that
   * ends the run and writes the cancelled status.
   */
  cancelSessionRun: (sessionId: string, reason?: string) =>
    request<{ cancelled: boolean }>(`/v1/sessions/${sessionId}/cancel`, {
      method: "POST",
      body: JSON.stringify(reason ? { reason } : {}),
    }),

  sendMessage: (
    sessionId: string,
    content: string,
    fileIds?: string[],
    orchestrate?: boolean,
    mentions?: MessageMention[],
    attachmentIds?: string[],
  ) =>
    request<AgentResponse>(`/v1/sessions/${sessionId}/messages`, {
      method: "POST",
      body: JSON.stringify({
        content,
        role: "user",
        file_ids: fileIds ?? [],
        ...(orchestrate ? { orchestrate: true } : {}),
        ...(mentions && mentions.length > 0 ? { mentions } : {}),
        ...(attachmentIds && attachmentIds.length > 0 ? { attachment_ids: attachmentIds } : {}),
      }),
    }),

  /**
   * Same endpoint as sendMessage with `"stream": true`: a text/event-stream body
   * instead of one JSON reply, so a long agent turn shows its answer as it is
   * written instead of sitting frozen. EventSource cannot serve this — it only
   * does GET and cannot carry the Authorization header — so it is fetch plus a
   * ReadableStream reader.
   *
   * Aborting `options.signal` (or breaking out of the for-await) cancels the
   * body. That is deliberate and must stay: the closed connection is what tells
   * the backend nobody is reading, and it cancels the agent loop there rather
   * than keep spending the user's budget on a reply nobody will see.
   */
  sendMessageStream: async function* (
    sessionId: string,
    content: string,
    options: {
      fileIds?: string[];
      orchestrate?: boolean;
      mentions?: MessageMention[];
      attachmentIds?: string[];
      signal?: AbortSignal;
    } = {},
  ): AsyncGenerator<ChatStreamEvent> {
    const { fileIds, orchestrate, mentions, attachmentIds, signal } = options;
    const res = await fetchWithTimeout(apiUrl(`/v1/sessions/${sessionId}/messages`), {
      method: "POST",
      headers: authHeaders({
        "Content-Type": "application/json",
        Accept: "text/event-stream",
      }),
      body: JSON.stringify({
        content,
        role: "user",
        stream: true,
        file_ids: fileIds ?? [],
        ...(orchestrate ? { orchestrate: true } : {}),
        ...(mentions && mentions.length > 0 ? { mentions } : {}),
        ...(attachmentIds && attachmentIds.length > 0 ? { attachment_ids: attachmentIds } : {}),
      }),
      signal,
    });
    if (!res.ok) {
      throw await apiErrorFrom(res);
    }
    if (!res.body) {
      throw new ApiError("streaming response has no body", res.status);
    }

    const reader = res.body.getReader();
    const bytes = new TextDecoder();
    const frames = createChatStreamDecoder();
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) {
          // A body that ended without its terminator may still hold a whole
          // trailing frame.
          yield* frames.flush();
          return;
        }
        // `stream: true` keeps a multi-byte character that straddles two network
        // chunks intact; without it every such character decodes as U+FFFD.
        for (const event of frames.push(bytes.decode(value, { stream: true }))) {
          yield event;
          if (event.kind === "done") return;
        }
      }
    } finally {
      // Also runs when the consumer breaks out of the loop or the signal fires:
      // release the body so the connection really closes.
      await reader.cancel().catch(() => {
        /* already closed */
      });
    }
  },

  sessionActivity: (sessionId: string) =>
    request<{ runs: SessionRun[] }>(`/v1/sessions/${sessionId}/activity`),

  runSteps: (runId: string) =>
    request<{ steps: SessionStep[] }>(`/v1/runs/${runId}/steps`),

  getRunPlan: (runId: string) => request<OrchestrationPlan>(`/v1/runs/${runId}/plan`),

  activeRuns: () => request<{ runs: SessionRun[] }>("/v1/activity/active"),

  listFiles: () => request<{ files: FileRecord[] }>("/v1/files"),

  uploadFile: async (file: File) => {
    const form = new FormData();
    form.append("file", file);
    // Goes through fetchWithTimeout like every other call: raw fetch
    // here meant an upload onto a sleeping workspace failed outright instead of
    // raising the waking overlay and retrying. Content-Type stays unset so the
    // browser writes the multipart boundary itself.
    const res = await fetchWithTimeout(apiUrl("/v1/files"), {
      method: "POST",
      headers: authHeaders(),
      body: form,
    });
    if (!res.ok) {
      throw new Error(await res.text());
    }
    return res.json() as Promise<FileRecord>;
  },

  deleteFile: (id: string) =>
    request<void>(`/v1/files/${id}`, { method: "DELETE" }),

  /**
   * Binary attachment upload (images/documents, stored in Postgres) — a
   * parallel concept to uploadFile above, which feeds the RAG text pipeline.
   * Mirrors uploadFile's raw fetch: FormData sets its own multipart boundary,
   * so no manual Content-Type.
   */
  uploadAttachment: async (file: File, repositoryId?: string) => {
    const form = new FormData();
    form.append("file", file);
    if (repositoryId) {
      form.append("repository_id", repositoryId);
    }
    const res = await fetchWithTimeout(apiUrl("/v1/attachments"), {
      method: "POST",
      headers: authHeaders(),
      body: form,
    });
    if (!res.ok) {
      throw await apiErrorFrom(res);
    }
    return res.json() as Promise<AttachmentMeta>;
  },

  deleteAttachment: (id: string) =>
    request<void>(`/v1/attachments/${id}`, { method: "DELETE" }),

  /**
   * Fetches an attachment's bytes with the Authorization header and returns a
   * Blob. <img>/<a> tags cannot carry the header, so rendering always goes
   * through this (see useAttachmentBlob) instead of attachmentUrl directly.
   */
  fetchAttachmentBlob: async (id: string) => {
    const res = await fetchWithTimeout(attachmentUrl(id), {
      method: "GET",
      headers: authHeaders(),
    });
    if (!res.ok) {
      throw await apiErrorFrom(res);
    }
    return res.blob();
  },

  listTaskAttachments: (repositoryId: string, taskId: string) =>
    request<{ attachments: AttachmentMeta[] }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/attachments`,
    ),

  linkTaskAttachment: (repositoryId: string, taskId: string, attachmentId: string) =>
    request<AttachmentMeta>(`/v1/repositories/${repositoryId}/tasks/${taskId}/attachments`, {
      method: "POST",
      body: JSON.stringify({ attachment_id: attachmentId }),
    }),

  unlinkTaskAttachment: (repositoryId: string, taskId: string, attachmentId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/tasks/${taskId}/attachments/${attachmentId}`, {
      method: "DELETE",
    }),

  listTools: (all?: boolean) =>
    request<{ tools: { function: { name: string; description: string } }[] }>(
      all ? "/v1/tools?all=true" : "/v1/tools",
    ),

  listAgentSkills: (agentId: string) =>
    request<{ skills: Skill[] }>(`/admin/agents/${agentId}/skills`),

  createAgentSkill: (agentId: string, skill: SkillInput) =>
    request<Skill>(`/admin/agents/${agentId}/skills`, { method: "POST", body: JSON.stringify(skill) }),

  getAgentSkill: (agentId: string, skillId: string) =>
    request<Skill>(`/admin/agents/${agentId}/skills/${skillId}`),

  updateAgentSkill: (agentId: string, skillId: string, skill: SkillInput) =>
    request<Skill>(`/admin/agents/${agentId}/skills/${skillId}`, {
      method: "PUT",
      body: JSON.stringify(skill),
    }),

  deleteAgentSkill: (agentId: string, skillId: string) =>
    request<void>(`/admin/agents/${agentId}/skills/${skillId}`, { method: "DELETE" }),

  listAgentTechStacks: (agentId: string) =>
    request<{ tech_stacks: TechStack[]; count: number }>(`/admin/agents/${agentId}/tech-stacks`),

  createAgentTechStack: (agentId: string, techStack: TechStackInput) =>
    request<TechStack>(`/admin/agents/${agentId}/tech-stacks`, {
      method: "POST",
      body: JSON.stringify(techStack),
    }),

  updateAgentTechStack: (agentId: string, techStackId: string, techStack: TechStackPatch) =>
    request<TechStack>(`/admin/agents/${agentId}/tech-stacks/${techStackId}`, {
      method: "PUT",
      body: JSON.stringify(techStack),
    }),

  deleteAgentTechStack: (agentId: string, techStackId: string) =>
    request<void>(`/admin/agents/${agentId}/tech-stacks/${techStackId}`, { method: "DELETE" }),

  listAgents: () => request<{ agents: Agent[]; seeding?: boolean }>("/admin/agents"),

  createAgent: (agent: AgentInput) =>
    request<Agent>("/admin/agents", { method: "POST", body: JSON.stringify(agent) }),

  getAgent: (id: string) => request<Agent>(`/admin/agents/${id}`),

  updateAgent: (id: string, agent: AgentInput) =>
    request<Agent>(`/admin/agents/${id}`, { method: "PUT", body: JSON.stringify(agent) }),

  deleteAgent: (id: string) => request<void>(`/admin/agents/${id}`, { method: "DELETE" }),

  listAgentTemplates: () => request<{ templates: AgentTemplate[] }>("/admin/agent-templates"),

  createAgentFromTemplate: (templateId: string, overrides?: Partial<AgentInput>) =>
    request<Agent>(`/admin/agents?template_id=${encodeURIComponent(templateId)}`, {
      method: "POST",
      body: JSON.stringify(overrides ?? {}),
    }),

  saveAgentAsTemplate: (agentId: string) =>
    request<AgentTemplate>(`/admin/agents/${agentId}/template`, { method: "POST" }),

  listAgentRules: (agentId: string) =>
    request<{ orchestrator_rules: OrchestratorRule[] }>(`/admin/agents/${agentId}/rules`),

  createAgentRule: (agentId: string, rule: OrchestratorRuleInput) =>
    request<OrchestratorRule>(`/admin/agents/${agentId}/rules`, {
      method: "POST",
      body: JSON.stringify(rule),
    }),

  getAgentRule: (agentId: string, ruleId: string) =>
    request<OrchestratorRule>(`/admin/agents/${agentId}/rules/${ruleId}`),

  updateAgentRule: (agentId: string, ruleId: string, rule: OrchestratorRuleInput) =>
    request<OrchestratorRule>(`/admin/agents/${agentId}/rules/${ruleId}`, {
      method: "PUT",
      body: JSON.stringify(rule),
    }),

  deleteAgentRule: (agentId: string, ruleId: string) =>
    request<void>(`/admin/agents/${agentId}/rules/${ruleId}`, { method: "DELETE" }),

  listKPIMetrics: () => request<{ metrics: KPIMetricInfo[] }>("/v1/kpi-metrics"),

  listAgentKPIs: (agentId: string) => request<{ kpis: AgentKPI[] }>(`/admin/agents/${agentId}/kpis`),

  createAgentKPI: (agentId: string, kpi: CreateKPIInput) =>
    request<AgentKPI>(`/admin/agents/${agentId}/kpis`, { method: "POST", body: JSON.stringify(kpi) }),

  updateAgentKPI: (agentId: string, kpiId: string, kpi: CreateKPIInput) =>
    request<AgentKPI>(`/admin/agents/${agentId}/kpis/${kpiId}`, { method: "PUT", body: JSON.stringify(kpi) }),

  deleteAgentKPI: (agentId: string, kpiId: string) =>
    request<void>(`/admin/agents/${agentId}/kpis/${kpiId}`, { method: "DELETE" }),

  getAgentPerformance: (agentId: string) =>
    request<AgentPerformance>(`/v1/agents/${agentId}/performance`),

  listAgentEvolutionEvents: (agentId: string, limit = 50) =>
    request<{ events: EvolutionEvent[] }>(`/v1/agents/${agentId}/evolution-events?limit=${limit}`),

  listAgentReflections: (agentId: string, limit = 20) =>
    request<{ reflections: AgentReflection[] }>(`/v1/agents/${agentId}/reflections?limit=${limit}`),

  reflectNow: (agentId: string) =>
    request<AgentReflection>(`/v1/agents/${agentId}/reflect`, { method: "POST" }),

  listAgentMemories: (
    agentId: string,
    scope: "agent" | "all" = "agent",
    filter: MemoryScopeFilter = {},
  ) => {
    const params = memoryScopeParams(filter);
    params.set("scope", scope);
    return request<{ memories: AgentMemory[] }>(`/v1/agents/${agentId}/memories?${params}`);
  },

  createAgentMemory: (agentId: string, body: MemoryInput) =>
    request<AgentMemory>(`/v1/agents/${agentId}/memories`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateAgentMemory: (agentId: string, memoryId: string, body: MemoryInput) =>
    request<AgentMemory>(`/v1/agents/${agentId}/memories/${memoryId}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  deleteAgentMemory: (agentId: string, memoryId: string) =>
    request<void>(`/v1/agents/${agentId}/memories/${memoryId}`, { method: "DELETE" }),

  listSharedMemories: (filter: MemoryScopeFilter = {}) =>
    request<{ memories: AgentMemory[] }>(`/v1/memories/shared?${memoryScopeParams(filter)}`),

  createSharedMemory: (body: MemoryInput) =>
    request<AgentMemory>("/v1/memories/shared", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateSharedMemory: (memoryId: string, body: MemoryInput) =>
    request<AgentMemory>(`/v1/memories/shared/${memoryId}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  deleteSharedMemory: (memoryId: string) =>
    request<void>(`/v1/memories/shared/${memoryId}`, { method: "DELETE" }),

  planSharedMemoryPromotion: () =>
    request<MemoryPromotionPlan>("/v1/memories/shared/promote/plan", { method: "POST" }),

  // With candidates, exactly those are written; without, the server plans and
  // applies everything (the pre-approval behaviour).
  promoteSharedMemories: (candidates?: MemoryPromotionCandidate[]) =>
    request<MemoryPromotionResult>("/v1/memories/shared/promote", {
      method: "POST",
      ...(candidates ? { body: JSON.stringify({ candidates }) } : {}),
    }),

  listMCPServers: () => request<{ servers: MCPServerView[] }>("/admin/mcp-servers"),

  createMCPServer: (server: MCPServerCreateInput) =>
    request<MCPServer>("/admin/mcp-servers", { method: "POST", body: JSON.stringify(server) }),

  updateMCPServer: (id: string, server: MCPServerUpdateInput) =>
    request<MCPServer>(`/admin/mcp-servers/${id}`, { method: "PUT", body: JSON.stringify(server) }),

  deleteMCPServer: (id: string) =>
    request<void>(`/admin/mcp-servers/${encodeURIComponent(id)}`, { method: "DELETE" }),

  listRepositories: () => request<{ repositories: Repository[] }>("/v1/repositories"),

  listProjects: () =>
    request<{ repositories: Repository[] }>("/v1/repositories").then((d) => ({
      projects: d.repositories,
    })),

  openRepository: (rootPath: string, description: string, projectIds?: string[]) =>
    request<Repository>("/v1/repositories/open", {
      method: "POST",
      body: JSON.stringify({ root_path: rootPath, description, project_ids: projectIds }),
    }),

  openProject: (rootPath: string, description: string) => api.openRepository(rootPath, description),

  createRepository: (
    name: string,
    parentDir: string,
    description: string,
    projectIds?: string[],
    owner?: string,
  ) =>
    request<Repository>("/v1/repositories", {
      method: "POST",
      body: JSON.stringify({ name, parent_dir: parentDir, description, project_ids: projectIds, owner }),
    }),

  createProject: (name: string, parentDir: string, description: string) =>
    api.createRepository(name, parentDir, description),

  getRepository: (id: string) => request<Repository>(`/v1/repositories/${id}`),

  /** Browses a repository's real directory tree (repo-relative, root when
   * `path` is omitted) — the folder picker behind manually adding a monorepo
   * sub-project. */
  listRepositoryDirectories: (id: string, path?: string) =>
    request<RepositoryDirectoryListing>(
      `/v1/repositories/${id}/directories${path ? `?path=${encodeURIComponent(path)}` : ""}`,
    ),

  /**
   * Fetch a registered repository's code onto this machine,
   * from the remote already on its record, into this runtime's own workspace.
   *
   * Returns as soon as the clone has STARTED (202) — the returned repository
   * carries `git_restore.status === "running"`; poll `getRepository` until it
   * settles. A refusal (folder already there, something else in the way, no
   * remote) is a 400 whose message is the reason and can be shown as-is.
   */
  restoreRepositoryWorkingCopy: (id: string) =>
    request<Repository>(`/v1/repositories/${id}/restore`, { method: "POST" }),

  getProject: (id: string) => api.getRepository(id),

  // NOTE: name and description are applied unconditionally by the backend —
  // an omitted description clears the stored one. Callers that only mean to
  // change one field must echo the others back.
  updateRepository: (
    id: string,
    data: {
      name?: string;
      description?: string;
      verify_command?: string;
      build_command?: string;
      test_command?: string;
      require_human_review?: boolean;
      kind?: RepoKind;
      sub_projects?: RepoSubProject[];
      docs?: RepositoryDocs;
      mobile_platform?: MobilePlatform;
      release_engine?: ReleaseEngine;
      mutation_enabled?: boolean;
      mutation_threshold?: number;
    },
  ) =>
    request<Repository>(`/v1/repositories/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),

  createRepoDocTask: (id: string, kind: RepoDocKind, opts?: { subProjectPath?: string; path?: string }) => {
    const params = new URLSearchParams();
    if (opts?.subProjectPath) params.set("sub_project_path", opts.subProjectPath);
    if (opts?.path) params.set("path", opts.path);
    const qs = params.toString();
    return request<BoardTask>(`/v1/repositories/${id}/docs/${kind}/setup-task${qs ? `?${qs}` : ""}`, { method: "POST" });
  },

  /** Kicks off one docs-setup task that writes/refreshes several reference
   * docs (repo-level and/or per-sub-project) in a single PR, unlike
   * createRepoDocTask which is one doc at a time. */
  createRepoDocsBundleTask: (repositoryId: string, items: Array<{ kind: RepoDocKind; sub_project_path?: string; path?: string }>) =>
    request<{ task_id: string }>(`/v1/repositories/${repositoryId}/docs/setup-task`, {
      method: "POST",
      body: JSON.stringify({ items }),
    }),

  getRepoDocsTask: (repositoryId: string) =>
    request<RepoDocsTaskStatus>(`/v1/repositories/${repositoryId}/docs/task`),

  mergeRepoDocsTask: (repositoryId: string) =>
    request<{ merged: boolean }>(`/v1/repositories/${repositoryId}/docs/task/merge`, { method: "POST" }),

  updateProject: (id: string, data: { name?: string; description?: string }) =>
    api.updateRepository(id, data),

  getRepositoryProfile: (id: string) =>
    request<RepositoryProfile>(`/v1/repositories/${id}/profile`),

  refreshRepositoryProfile: (id: string) =>
    request<{ status: "started" | "already_running" }>(`/v1/repositories/${id}/profile/refresh`, {
      method: "POST",
    }),

  applyProfileProposal: (id: string, proposalId: string) =>
    request<ProfileProposal>(`/v1/repositories/${id}/profile/proposals/${proposalId}/apply`, {
      method: "POST",
    }),

  dismissProfileProposal: (id: string, proposalId: string) =>
    request<ProfileProposal>(`/v1/repositories/${id}/profile/proposals/${proposalId}/dismiss`, {
      method: "POST",
    }),

  getPipelineConfig: (id: string) =>
    request<PipelineConfigView>(`/v1/repositories/${id}/pipeline/config`),

  savePipelineConfig: (id: string, data: SavePipelineConfigInput) =>
    request<PipelineConfigView>(`/v1/repositories/${id}/pipeline/config`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),

  createWorkflowSetupTask: (id: string) =>
    request<BoardTask>(`/v1/repositories/${id}/pipeline/setup-task`, { method: "POST" }),

  getDeployConfig: (id: string, subProjectPath?: string) =>
    request<DeployConfigView>(
      `/v1/repositories/${id}/deploy/config${subProjectPath ? `?sub_project_path=${encodeURIComponent(subProjectPath)}` : ""}`,
    ),

  saveDeployTarget: (id: string, data: SaveDeployTargetInput) =>
    request<DeployTarget>(`/v1/repositories/${id}/deploy/targets`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),

  deleteDeployTarget: (id: string, env: DeployEnv, subProjectPath?: string) =>
    request<void>(
      `/v1/repositories/${id}/deploy/targets/${env}${subProjectPath ? `?sub_project_path=${encodeURIComponent(subProjectPath)}` : ""}`,
      { method: "DELETE" },
    ),

  getDeployInstructions: (id: string, env: DeployEnv) =>
    request<{ env: string; instructions: string }>(
      `/v1/repositories/${id}/deploy/targets/${env}/instructions`),

  createDeploySetupTask: (id: string, env: DeployEnv) =>
    request<BoardTask>(`/v1/repositories/${id}/deploy/targets/${env}/setup-task`, { method: "POST" }),

  createLocalSetupTask: (id: string, subProjectPath?: string) =>
    request<BoardTask>(
      `/v1/repositories/${id}/deploy/local-setup-task${subProjectPath ? `?sub_project_path=${encodeURIComponent(subProjectPath)}` : ""}`,
      { method: "POST" },
    ),

  listDeployTemplates: (kind?: string) =>
    request<{ templates: DeployTemplate[]; count: number }>(
      `/v1/deploy/templates${kind ? `?kind=${encodeURIComponent(kind)}` : ""}`),

  setTestStrategy: (id: string, strategy: TestStrategy) =>
    request<Repository>(`/v1/repositories/${id}/test-strategy`, {
      method: "PUT",
      body: JSON.stringify({ test_strategy: strategy }),
    }),

  // Deploy packages — release trains for repositories that turned per-task
  // auto release off. Listing is not a pure read: the server advances every
  // package on the way out (deploys land out of band), so poll this after any
  // action rather than patching the response into local state.
  listDeployPackages: (repositoryId: string) =>
    request<{ packages: DeployPackage[]; count: number }>(
      `/v1/repositories/${repositoryId}/deploy-packages`),

  createDeployPackage: (repositoryId: string, data: { name: string; description?: string }) =>
    request<DeployPackage>(`/v1/repositories/${repositoryId}/deploy-packages`, {
      method: "POST",
      body: JSON.stringify(data),
    }),

  updateDeployPackage: (
    repositoryId: string,
    packageId: string,
    data: { name?: string; description?: string; status?: "cancelled" },
  ) =>
    request<DeployPackage>(`/v1/repositories/${repositoryId}/deploy-packages/${packageId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),

  deleteDeployPackage: (repositoryId: string, packageId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/deploy-packages/${packageId}`, {
      method: "DELETE",
    }),

  setDeployPackageTasks: (repositoryId: string, packageId: string, taskIds: string[]) =>
    request<DeployPackage>(`/v1/repositories/${repositoryId}/deploy-packages/${packageId}/tasks`, {
      method: "PUT",
      body: JSON.stringify({ task_ids: taskIds }),
    }),

  releaseDeployPackage: (repositoryId: string, packageId: string) =>
    request<DeployPackage>(`/v1/repositories/${repositoryId}/deploy-packages/${packageId}/release`, {
      method: "POST",
    }),

  setLifecycleGates: (
    id: string,
    data: {
      require_review_chain?: boolean;
      require_release_deploy?: boolean;
      require_pipeline_for_review?: boolean;
      require_overall_coverage?: boolean;
      coverage_threshold?: number;
    },
  ) =>
    request<Repository>(`/v1/repositories/${id}/lifecycle-gates`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),

  getEnvInventory: (id: string) => request<EnvInventory>(`/v1/repositories/${id}/env-inventory`),

  setIncidentPolicy: (id: string, policy: IncidentPolicy) =>
    request<Repository>(`/v1/repositories/${id}/incident-policy`, {
      method: "PUT",
      body: JSON.stringify({ incident_policy: policy }),
    }),

  saveStoreCredential: (provider: StoreCredentialProvider, data: Record<string, string>) =>
    request<void>(`/v1/store/credentials/${provider}`, {
      method: "PUT",
      body: JSON.stringify({ data }),
    }),

  listStoreCredentials: () => request<StoreCredentialView[]>("/v1/store/credentials"),

  deleteStoreCredential: (provider: StoreCredentialProvider) =>
    request<void>(`/v1/store/credentials/${provider}`, { method: "DELETE" }),

  listStoreApps: (id: string) => request<MobileStoreApp[]>(`/v1/repositories/${id}/store/apps`),

  listStoreCredentialApps: (provider: StoreCredentialProvider) =>
    request<StoreAppListing>(`/v1/store/credentials/${provider}/apps`),

  linkStoreApp: (id: string, platform: MobileStorePlatform, app: StoreAppRef) =>
    request<MobileStoreApp>(`/v1/repositories/${id}/store/apps/${platform}/link`, {
      method: "PUT",
      body: JSON.stringify({ identifier: app.identifier, store_app_id: app.store_app_id, name: app.name }),
    }),

  storeAppTracks: (id: string, platform: MobileStorePlatform) =>
    request<StoreTracks>(`/v1/repositories/${id}/store/apps/${platform}/tracks`),

  promoteStoreChannel: (
    id: string,
    platform: MobileStorePlatform,
    from: StoreChannel,
    to: StoreChannel,
    confirm?: string,
  ) =>
    request<void>(`/v1/repositories/${id}/store/apps/${platform}/promote`, {
      method: "POST",
      body: JSON.stringify({ from, to, confirm: confirm ?? "" }),
    }),

  startStoreBuild: (id: string, platform: MobileStorePlatform, engine?: ReleaseEngine) =>
    request<BuildStart>(`/v1/repositories/${id}/store/apps/${platform}/build`, {
      method: "POST",
      body: JSON.stringify({ engine: engine ?? "" }),
    }),

  verifyStoreOnboarding: (id: string, platform: MobileStorePlatform) =>
    request<MobileStoreApp>(`/v1/repositories/${id}/store/apps/${platform}/verify`, {
      method: "POST",
    }),

  // Operations console: deploy matrix, run history, dispatch/rollback, audit
  // log, and cross-repository store app actions.
  getDeployMatrix: () => request<MatrixView>("/v1/operations/deployments"),

  // GET /v1/repositories/:id/deploy/:env/runs returns the run list as a bare
  // JSON array (handler_deployops.go ListDeployRuns: `c.JSON(runs)`), not the
  // `{runs: [...]}` envelope — matched here accordingly.
  listDeployRuns: (id: string, env: DeployEnv, limit = 20) =>
    request<DeploymentRun[] | null>(
      `/v1/repositories/${id}/deploy/${env}/runs?limit=${limit}`),

  dispatchDeploy: (id: string, env: DeployEnv, body: { ref?: string; confirm?: string }) =>
    request<void>(`/v1/repositories/${id}/deploy/${env}/dispatch`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  rollbackDeploy: (id: string, env: DeployEnv, confirm: string) =>
    request<void>(`/v1/repositories/${id}/deploy/${env}/rollback`, {
      method: "POST",
      body: JSON.stringify({ confirm }),
    }),

  // GET /v1/operations/audit returns a bare JSON array (handler_deployops.go
  // ListOpsAudit: `c.JSON(entries)`), not `{entries: [...]}`.
  listOpsAudit: (repositoryId?: string, limit = 50) =>
    request<OpsAuditEntry[] | null>(
      `/v1/operations/audit?limit=${limit}${repositoryId ? `&repository_id=${repositoryId}` : ""}`),

  listAllStoreApps: () => request<StoreAppView[] | null>("/v1/operations/apps"),

  submitIOS: (id: string, confirm: string) =>
    request<void>(`/v1/repositories/${id}/store/apps/ios/submit`, {
      method: "POST", body: JSON.stringify({ confirm }),
    }),

  releaseIOS: (id: string, confirm: string) =>
    request<void>(`/v1/repositories/${id}/store/apps/ios/release`, {
      method: "POST", body: JSON.stringify({ confirm }),
    }),

  promoteAndroid: (id: string, body: { to_track: string; user_fraction: number; confirm?: string }) =>
    request<void>(`/v1/repositories/${id}/store/apps/android/promote`, {
      method: "POST", body: JSON.stringify(body),
    }),

  setAndroidRollout: (id: string, userFraction: number) =>
    request<void>(`/v1/repositories/${id}/store/apps/android/rollout`, {
      method: "POST", body: JSON.stringify({ user_fraction: userFraction }),
    }),

  haltAndroid: (id: string, confirm: string) =>
    request<void>(`/v1/repositories/${id}/store/apps/android/halt`, {
      method: "POST", body: JSON.stringify({ confirm }),
    }),

  resumeAndroid: (id: string) =>
    request<void>(`/v1/repositories/${id}/store/apps/android/resume`, { method: "POST" }),

  listIncidents: (params?: { repositoryId?: string; env?: string; status?: string }) => {
    const query = new URLSearchParams();
    if (params?.repositoryId) query.set("repository_id", params.repositoryId);
    if (params?.env) query.set("env", params.env);
    if (params?.status) query.set("status", params.status);
    const qs = query.toString();
    return request<{ incidents: Incident[] | null; count: number }>(
      `/v1/incidents${qs ? `?${qs}` : ""}`);
  },

  getIncident: (incidentId: string) => request<Incident>(`/v1/incidents/${incidentId}`),

  triageIncident: (incidentId: string) =>
    request<Incident>(`/v1/incidents/${incidentId}/triage`, { method: "POST" }),

  resolveIncident: (incidentId: string, note?: string) =>
    request<Incident>(`/v1/incidents/${incidentId}/resolve`, {
      method: "POST",
      body: JSON.stringify({ note: note ?? "" }),
    }),

  ignoreIncident: (incidentId: string, note?: string) =>
    request<Incident>(`/v1/incidents/${incidentId}/ignore`, {
      method: "POST",
      body: JSON.stringify({ note: note ?? "" }),
    }),

  deleteRepository: (id: string) =>
    request<void>(`/v1/repositories/${id}`, { method: "DELETE" }),

  deleteProject: (id: string) => api.deleteRepository(id),

  setRepositoryProjects: (id: string, projectIds: string[]) =>
    request<Repository>(`/v1/repositories/${id}/projects`, {
      method: "PUT",
      body: JSON.stringify({ project_ids: projectIds }),
    }),

  getRepositoryIndexStatus: (repositoryId: string) =>
    request<WorkspaceIndex>(`/v1/repositories/${repositoryId}/index/status`),

  getProjectIndexStatus: (repositoryId: string) => api.getRepositoryIndexStatus(repositoryId),

  reindexRepository: (repositoryId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/index`, { method: "POST" }),

  setupRepositoryWebhook: (repositoryId: string) =>
    request<Repository>(`/v1/repositories/${repositoryId}/webhook`, { method: "POST" }),

  searchRepositoryIndex: (repositoryId: string, query: string, topK = 10) =>
    request<{ results: WorkspaceChunk[]; count: number }>(
      `/v1/repositories/${repositoryId}/index/search`,
      { method: "POST", body: JSON.stringify({ query, top_k: topK }) },
    ),

  stopRepositoryIndex: (repositoryId: string) =>
    request<{ stopped: boolean }>(`/v1/repositories/${repositoryId}/index`, { method: "DELETE" }),
  reindexProject: (repositoryId: string) => api.reindexRepository(repositoryId),

  getEmbeddingMapSources: () => request<EmbeddingMapSources>("/v1/embedding-map/sources"),

  getEmbeddingMap: (params: {
    source: EmbeddingMapSource;
    repositoryId?: string;
    limit?: number;
    dims?: number;
  }) => {
    const query = new URLSearchParams({ source: params.source });
    if (params.repositoryId) query.set("repository_id", params.repositoryId);
    if (params.limit) query.set("limit", String(params.limit));
    if (params.dims) query.set("dims", String(params.dims));
    return request<EmbeddingMapResponse>(`/v1/embedding-map?${query.toString()}`);
  },

  listRepositoryTasks: (repositoryId: string) =>
    request<{ tasks: BoardTask[] }>(`/v1/repositories/${repositoryId}/tasks`),

  listProjectTasks: (repositoryId: string) => api.listRepositoryTasks(repositoryId),

  createRepositoryTask: (repositoryId: string, data: CreateBoardTaskInput) =>
    request<BoardTask>(`/v1/repositories/${repositoryId}/tasks`, {
      method: "POST",
      body: JSON.stringify(data),
    }),

  createProjectTask: (
    repositoryId: string,
    data: { title: string; description?: string; column?: TaskColumn },
  ) => api.createRepositoryTask(repositoryId, data),

  updateRepositoryTask: (repositoryId: string, taskId: string, data: UpdateBoardTaskInput) =>
    request<BoardTask>(`/v1/repositories/${repositoryId}/tasks/${taskId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),

  updateProjectTask: (repositoryId: string, taskId: string, data: UpdateBoardTaskInput) =>
    api.updateRepositoryTask(repositoryId, taskId, data),

  deleteRepositoryTask: (repositoryId: string, taskId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/tasks/${taskId}`, { method: "DELETE" }),

  deleteProjectTask: (repositoryId: string, taskId: string) =>
    api.deleteRepositoryTask(repositoryId, taskId),

  listTaskDocuments: (repositoryId: string, taskId: string) =>
    request<{ documents: TaskDocument[] }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/documents`,
    ),

  createTaskDocument: (repositoryId: string, taskId: string, data: CreateTaskDocumentInput) =>
    request<TaskDocument>(`/v1/repositories/${repositoryId}/tasks/${taskId}/documents`, {
      method: "POST",
      body: JSON.stringify(data),
    }),

  updateTaskDocument: (
    repositoryId: string,
    taskId: string,
    docId: string,
    data: UpdateTaskDocumentInput,
  ) =>
    request<TaskDocument>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/documents/${docId}`,
      { method: "PATCH", body: JSON.stringify(data) },
    ),

  deleteTaskDocument: (repositoryId: string, taskId: string, docId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/tasks/${taskId}/documents/${docId}`, {
      method: "DELETE",
    }),

  listAcceptanceCriteria: (repositoryId: string, taskId: string) =>
    request<{ items: AcceptanceCriterion[] }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/acceptance-criteria`,
    ),

  replaceAcceptanceCriteria: (
    repositoryId: string,
    taskId: string,
    items: AcceptanceCriterionInput[],
  ) =>
    request<{ items: AcceptanceCriterion[] }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/acceptance-criteria`,
      { method: "PUT", body: JSON.stringify({ items }) },
    ),

  updateAcceptanceCriterion: (
    repositoryId: string,
    taskId: string,
    criterionId: string,
    completed: boolean,
  ) =>
    request<AcceptanceCriterion>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/acceptance-criteria/${criterionId}`,
      { method: "PATCH", body: JSON.stringify({ completed }) },
    ),

  // Cancelling is its own call, not a flavour of the tick: it carries a reason,
  // and the server posts that reason as a task comment.
  cancelAcceptanceCriterion: (
    repositoryId: string,
    taskId: string,
    criterionId: string,
    canceled: boolean,
    cancelReason: string,
  ) =>
    request<AcceptanceCriterion>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/acceptance-criteria/${criterionId}`,
      { method: "PATCH", body: JSON.stringify({ canceled, cancel_reason: cancelReason }) },
    ),

  listTestCases: (repositoryId: string, taskId: string) =>
    request<{ items: TaskTestCase[]; summary: TestCaseSummary }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/test-cases`,
    ),

  replaceTestCases: (repositoryId: string, taskId: string, items: TaskTestCaseInput[]) =>
    request<{ items: TaskTestCase[]; summary: TestCaseSummary }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/test-cases`,
      { method: "PUT", body: JSON.stringify({ items }) },
    ),

  updateTestCase: (
    repositoryId: string,
    taskId: string,
    testCaseId: string,
    patch: Partial<TaskTestCaseInput>,
  ) =>
    request<TaskTestCase>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/test-cases/${testCaseId}`,
      { method: "PATCH", body: JSON.stringify(patch) },
    ),

  deleteTestCase: (repositoryId: string, taskId: string, testCaseId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/tasks/${taskId}/test-cases/${testCaseId}`, {
      method: "DELETE",
    }),

  lookupTask: (key: string) =>
    request<BoardTask>(`/v1/tasks/lookup?key=${encodeURIComponent(key)}`),

  listInitiativeProjects: () =>
    request<{ projects: InitiativeProject[] }>("/v1/projects"),

  createInitiativeProject: (data: { name: string; description?: string }) =>
    request<InitiativeProject>("/v1/projects", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  getInitiativeProject: (projectId: string) =>
    request<InitiativeProject>(`/v1/projects/${projectId}`),

  updateInitiativeProject: (
    projectId: string,
    data: { name?: string; description?: string },
  ) =>
    request<InitiativeProject>(`/v1/projects/${projectId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),

  deleteInitiativeProject: (projectId: string) =>
    request<void>(`/v1/projects/${projectId}`, { method: "DELETE" }),

  getWorkspaceConfig: () => request<WorkspaceConfig>("/v1/board/config"),

  getBoardSettings: () => request<BoardSettings>("/v1/board/settings"),

  updateBoardSettings: (keyPrefix: string) =>
    request<BoardSettings>("/v1/board/settings", {
      method: "PUT",
      body: JSON.stringify({ key_prefix: keyPrefix }),
    }),

  listBoardColumns: () =>
    request<{ columns: BoardColumn[] }>("/v1/board/columns"),

  updateBoardColumns: (
    columns: { slug: string; label: string; position: number; is_backlog: boolean }[],
  ) =>
    request<{ columns: BoardColumn[] }>("/v1/board/columns", {
      method: "PUT",
      body: JSON.stringify({ columns }),
    }),

  listBoardMembers: () =>
    request<{ members: BoardMember[] }>("/v1/board/members"),

  setBoardMembers: (agentIds: string[]) =>
    request<void>("/v1/board/members", {
      method: "PUT",
      body: JSON.stringify({ agent_ids: agentIds }),
    }),

  listBoardSubscriptions: () =>
    request<{ subscriptions: BoardSubscription[] }>("/v1/board/subscriptions"),

  setBoardSubscriptions: (
    subscriptions: { agent_id: string; column_slugs: string[] }[],
  ) =>
    request<void>("/v1/board/subscriptions", {
      method: "PUT",
      body: JSON.stringify({ subscriptions }),
    }),

  listActivity: (limit = 50) =>
    request<{ items: ActivityItem[] }>(`/v1/activity?limit=${limit}`),

  getBoardTransitions: () =>
    request<{ transitions: BoardTransition[] }>("/v1/board/transitions"),

  setBoardTransitions: (transitions: BoardTransition[]) =>
    request<void>("/v1/board/transitions", {
      method: "PUT",
      body: JSON.stringify({ transitions }),
    }),

  getAgentSubscriptions: (agentId: string) =>
    request<{ column_slugs: string[] }>(`/v1/agents/${agentId}/subscriptions`),

  setAgentSubscriptions: (agentId: string, columnSlugs: string[]) =>
    request<void>(`/v1/agents/${agentId}/subscriptions`, {
      method: "PUT",
      body: JSON.stringify({ column_slugs: columnSlugs }),
    }),

  listAllTasks: () =>
    request<{ tasks: BoardTask[] }>("/v1/tasks"),

  // The released column in full — the board itself only carries the last week
  // of it. `query` matches the board key, the title or the description.
  listReleasedTasks: (query = "", limit = 100) => {
    const params = new URLSearchParams();
    if (query.trim()) params.set("q", query.trim());
    params.set("limit", String(limit));
    return request<{ tasks: BoardTask[] }>(`/v1/tasks/released?${params.toString()}`);
  },

  listTaskComments: (repositoryId: string, taskId: string) =>
    request<{ comments: TaskComment[] }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/comments`,
    ),

  createTaskComment: (repositoryId: string, taskId: string, content: string) =>
    request<TaskComment>(`/v1/repositories/${repositoryId}/tasks/${taskId}/comments`, {
      method: "POST",
      body: JSON.stringify({ content }),
    }),

  /**
   * Opens (or reuses) the chat thread that discusses this task and the pull
   * request opened for it. The agent it answers with is the one that owns the
   * session, so both ids are needed to reach `/agents/:agentId/chat/:sessionId`.
   */
  openTaskChat: (repositoryId: string, taskId: string) =>
    request<{ session_id: string; agent_id: string }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/chat`,
      { method: "POST" },
    ),

  /**
   * Starts (or restarts) the repository's local preview on this task's
   * branch — the same checkout `openTaskChat`'s agent works in, run instead
   * of talked to. Only one preview runs per repository; starting a second
   * one replaces the first.
   */
  startLocalPreview: (repositoryId: string, taskId: string) =>
    request<LocalPreview>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/local-preview/start`,
      { method: "POST" },
    ),

  stopLocalPreview: (repositoryId: string) =>
    request<void>(`/v1/repositories/${repositoryId}/local-preview/stop`, { method: "POST" }),

  /** The repository's current preview, whichever task started it. */
  getLocalPreview: (repositoryId: string) =>
    request<{ active: boolean; preview?: LocalPreview }>(
      `/v1/repositories/${repositoryId}/local-preview`,
    ),

  listTaskAgentRuns: (repositoryId: string, taskId: string) =>
    request<{ runs: TaskAgentRun[] }>(`/v1/repositories/${repositoryId}/tasks/${taskId}/runs`),

  cancelTaskAgentRun: (repositoryId: string, taskId: string, runId: string, reason?: string) =>
    request<{ run: TaskAgentRun }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/runs/${runId}/cancel`,
      {
        method: "POST",
        body: JSON.stringify(reason ? { reason } : {}),
      },
    ),

  /** Returns a brand-new run; the cancelled/failed one stays in the list as history. */
  rerunTaskAgentRun: (repositoryId: string, taskId: string, runId: string) =>
    request<{ run: TaskAgentRun }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/runs/${runId}/rerun`,
      { method: "POST" },
    ),

  listTaskEvents: (repositoryId: string, taskId: string) =>
    request<{ events: TaskEvent[] }>(`/v1/repositories/${repositoryId}/tasks/${taskId}/events`),

  listTaskPipelines: (repositoryId: string, taskId: string) =>
    request<{ pipelines: TaskPipeline[] }>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/pipelines`),

  getTaskPipeline: (repositoryId: string, taskId: string, pipelineId: string) =>
    request<TaskPipeline>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/pipelines/${pipelineId}`),

  triggerTaskPipeline: (repositoryId: string, taskId: string) =>
    request<TaskPipeline>(
      `/v1/repositories/${repositoryId}/tasks/${taskId}/pipelines`, { method: "POST" }),
};
