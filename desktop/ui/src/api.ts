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

/** A column subscription may also filter which task types wake the agent; null = every type. */
export interface AgentColumnSubscription {
  column_slug: string;
  task_types: string[] | null;
}

export interface AgentSubscriptionsResponse {
  column_slugs: string[];
  subscriptions: AgentColumnSubscription[];
}

/** Per-agent prompt handed to the agent when a task arrives in a column. */
export interface AgentColumnInstruction {
  column_slug: string;
  instruction: string;
}

export type RoleArea = string;

/** Well-known repo areas offered as presets; any other string is a valid area too. */
export const ROLE_AREAS: RoleArea[] = ["backend", "frontend", "mobile"];

/** One behaviour attached to a task type or a workflow stage; params keyed by BehaviourSpec.params[].name. */
export interface BehaviourRef {
  key: string;
  params?: Record<string, string>;
}

export interface RoleAssignment {
  agent_id: string;
  /** Read-only, filled by the server for display. */
  agent_name?: string;
  /** null = any area. */
  areas: RoleArea[] | null;
  priority: number;
}

export interface Role {
  id: string;
  key: string;
  name: string;
  description: string;
  required_tools: string[];
  assignments: RoleAssignment[];
  /** Purpose keys (system_task_assignee, repo_profiler) currently pointed at this role. */
  purposes: string[];
}

export interface CreateRoleInput {
  key: string;
  name: string;
  description?: string;
  required_tools?: string[];
}

export interface UpdateRoleInput {
  name: string;
  description?: string;
  required_tools?: string[];
}

export interface SetRoleAssignmentsInput {
  assignments: { agent_id: string; areas: RoleArea[] | null; priority: number }[];
  confirm_grant_tools?: boolean;
}

// 422 (missing_tools, saved:false) is a normal outcome here, not an error —
// the caller opens a confirm-and-grant dialog. Mirrors AnalizAssignmentResult's
// old shape, generalized to any role.
export interface RoleAssignmentsResult {
  saved: boolean;
  role?: Role;
  granted_tools?: Record<string, string[]>;
  missing_tools?: Record<string, string[]>;
}

export interface AgentRoleRef {
  role_id: string;
  key: string;
  name: string;
  areas: RoleArea[] | null;
}

export interface SetAgentRolesInput {
  roles: { role_id: string; areas: RoleArea[] | null }[];
  confirm_grant_tools?: boolean;
}

export interface AgentRolesResult {
  saved: boolean;
  roles?: AgentRoleRef[];
  granted_tools?: Record<string, string[]>;
  missing_tools?: Record<string, string[]>;
}

/** Hook names, not roles: which role currently answers a system-created task / repo profile run. */
export type RolePurposeKey = "system_task_assignee" | "repo_profiler";

export const ROLE_PURPOSE_KEYS: RolePurposeKey[] = ["system_task_assignee", "repo_profiler"];

export interface RolePurposeAssignment {
  purpose: RolePurposeKey;
  role_id: string | null;
}

export type AssigneeMode = "none" | "default" | "override";

export interface TaskTypeDef {
  key: string;
  label: string;
  key_prefix: string;
  position: number;
  is_default: boolean;
  is_defect: boolean;
  assignee_role_id?: string | null;
  assignee_mode: AssigneeMode;
  behaviours: BehaviourRef[];
  built_in: boolean;
  task_count: number;
}

export interface CreateTaskTypeInput {
  key: string;
  label: string;
  key_prefix: string;
  /** Copies label/prefix/behaviours/stages from an existing type as a starting point. */
  clone_from?: string;
}

export interface UpdateTaskTypeInput {
  label: string;
  key_prefix: string;
  position: number;
  is_default: boolean;
  is_defect: boolean;
  assignee_role_id?: string | null;
  assignee_mode: AssigneeMode;
  behaviours: BehaviourRef[];
}

export type StageKind =
  | "intake"
  | "queue"
  | "work"
  | "review"
  | "approval"
  | "rework"
  | "parked"
  | "terminal";

export const STAGE_KINDS: StageKind[] = [
  "intake",
  "queue",
  "work",
  "review",
  "approval",
  "rework",
  "parked",
  "terminal",
];

export type StageParticipantMode = "worker" | "approver";

export interface StageParticipant {
  role_id: string;
  mode: StageParticipantMode;
  instructions: string;
  position: number;
}

export interface WorkflowStage {
  id?: string;
  column_slug: string;
  position: number;
  on_path: boolean;
  kind: StageKind;
  behaviours: BehaviourRef[];
  instructions: string;
  participants: StageParticipant[];
}

export interface TaskTypeWorkflow {
  task_type: string;
  stages: WorkflowStage[];
}

export interface WorkflowProblem {
  column_slug: string;
  field: string;
  message: string;
}

// 422 with `problems` is a normal outcome (inline per-stage/field errors), not
// a thrown error.
export interface UpdateWorkflowResult {
  saved: boolean;
  stages?: WorkflowStage[];
  error?: string;
  problems?: WorkflowProblem[];
}

export type BehaviourParamType = "column" | "string" | "enum" | "bool";

export interface BehaviourParamSpec {
  name: string;
  type: BehaviourParamType;
  options?: string[];
  required: boolean;
}

export type BehaviourScope = "stage" | "type";

// When a stage-scoped behaviour's effect fires — a presentation grouping for
// the Settings UI, not a new firing rule; every behaviour already fires at
// its own fixed point in the engine. A type-scoped behaviour has no
// meaningful entry/exit and is always "other".
export type BehaviourGroup = "entry" | "exit" | "other";

export interface BehaviourSpec {
  key: string;
  scope: BehaviourScope;
  group: BehaviourGroup;
  /** Stage kinds this behaviour can fire on; empty means every kind. */
  kinds: StageKind[];
  label: string;
  description: string;
  params: BehaviourParamSpec[];
}

export interface WorkflowBehavioursResponse {
  behaviours: BehaviourSpec[];
  kinds: StageKind[];
  areas: RoleArea[];
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
//   gate_disabled    — history only: the pipeline gate can no longer be
//                       disabled, so only events recorded before it became
//                       unconditional carry this reason
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
  /** The git origin the working copy came from; absent for a repo with no remote on record. */
  remote_url?: string;
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
  require_human_review?: boolean;
  incident_policy?: IncidentPolicy;
  test_strategy?: TestStrategy;
  docs?: RepositoryDocs;
  webhook_installed?: boolean;
  mobile_platform?: MobilePlatform;
  release_engine?: ReleaseEngine;
  created_at: string;
  updated_at: string;
}

export type DeployEnv = "local" | "stage" | "preprod" | "prod";

export type TestStrategy = "local" | "stage" | "per_step";

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
export type Project = Repository;

// Task types are data now (task_types table), not a closed enum — see
// TaskTypeDef / api.listTaskTypes. Built-in keys (task/analiz/bug/technical)
// keep their special treatment only in lib/project-board's label mapping.
export type TaskType = string;

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
   * The shared resource the task is parked on — "llm_provider_code_quota",
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
  /** Concurrency caps for the board runner; 0 (or absent) means unlimited. */
  max_concurrent_agents?: number;
  max_concurrent_tasks?: number;
}

export interface GitHubConnectionStatus {
  connected: boolean;
  login?: string;
  detail?: string;
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
  catalog_slug?: string;
  catalog_etag?: string;
  // Optional because a server older than this feature omits them; absent reads
  // as "born connected to the catalog" in the settings page.
  auto_pull_agent_updates?: boolean;
  keep_skills_updated?: boolean;
  created_at: string;
}

// The two catalog sync gates are optional on write so a partial save that never
// meant to touch them (e.g. the performance page's model swap) cannot quietly
// switch a synced agent back to "auto-pull everything".
export type AgentInput = Omit<
  Agent,
  "id" | "created_at" | "skill_ids" | "auto_pull_agent_updates" | "keep_skills_updated"
> &
  Partial<Pick<Agent, "auto_pull_agent_updates" | "keep_skills_updated">>;

export interface CatalogSyncResult {
  repo_ref: string;
  created: number;
  updated: number;
  merged: number;
  skipped: number;
  pending: number;
}

export interface CatalogSyncState {
  repo_ref: string;
  last_sync_at: string;
  last_error?: string;
  last_summary?: CatalogSyncResult | null;
  pending_count: number;
  updated_at: string;
}

export interface CatalogPending {
  id: string;
  agent_slug: string;
  agent_name: string;
  kind: string;
  name: string;
  action: string;
  reason: string;
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

export interface PerformanceSnapshot {
  score: number;
  kpi_composite: number;
  /** metric_key -> attainment (0..1). */
  kpis?: Record<string, number>;
  /** 0..1. */
  golden_pass_rate?: number;
  /** 0..1. The rate this snapshot's golden_pass_rate is compared against. */
  golden_pass_rate_before?: number;
  captured_at: string;
}

export type ReflectionOutcome = "applied" | "rejected" | "skipped" | "failed" | "rolled_back" | "not_applied";

interface ReflectionCatalogCounts {
  skills: number;
  rules: number;
  memories: number;
  skill_budget: number;
  rule_budget: number;
}

export interface ReflectionChange {
  kind: "skill" | "rule" | "memory" | "revert";
  action: "create" | "update" | "delete" | "revert";
  name: string;
  target_id?: string;
  reason?: string;
  outcome: ReflectionOutcome;
  detail?: string;
  event_id?: string;
}

export interface ReflectionDecision {
  /** Model's markdown prose. */
  analysis?: string;
  /** 2-4 sentence summary. */
  self_assessment?: string;
  /** Previous analysis' snapshot ("before"). */
  baseline?: PerformanceSnapshot;
  catalog_before: ReflectionCatalogCounts;
  catalog_after: ReflectionCatalogCounts;
  /** `null` on the wire, not `[]`: a decision that changed nothing is stored
   *  with a nil slice, and Go marshals that as null. */
  changes: ReflectionChange[] | null;
  gate?: { before_rate: number; after_rate: number; keep: boolean; reason?: string; rolled_back: number };
  /** An older analysis parsed after the fact; its proposals were never applied. */
  legacy?: boolean;
}

export interface AgentReflection {
  id: string;
  agent_id: string;
  trigger: string;
  status: "running" | "completed" | "failed";
  window_start: string;
  window_end: string;
  summary: string;
  performance_snapshot?: PerformanceSnapshot;
  raw_output?: string;
  decision?: ReflectionDecision;
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

export interface MCPConfigField {
  key: string;
  location: "env" | "headers" | "args";
  secret?: boolean;
  required?: boolean;
  description?: string;
}

export interface MCPServerView extends MCPServer {
  connected: boolean;
  tool_count: number;
  tools?: string[];
  status: "disabled" | "connected" | "error" | "needs_config";
  last_error?: string;
  env_schema?: MCPEnvSchemaField[];
  secret_fields?: string[];
  config_fields?: MCPConfigField[];
  missing_config?: MCPConfigField[];
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
    const parsed = JSON.parse(text) as {
      error?: string | { message?: string; type?: string };
      code?: string;
    };
    if (typeof parsed.error === "string") {
      message = parsed.error;
    } else {
      message = parsed.error?.message ?? text;
      type = parsed.error?.type ?? "";
    }
    // Some coded errors (e.g. the cloud endpoints' cloud_auth) carry the code
    // only at the top level, `{error, code}`, rather than nested in error.type.
    if (!type && parsed.code) type = parsed.code;
  } catch {
    /* keep text */
  }
  return new ApiError(message || `HTTP ${res.status}`, res.status, type);
}

/** A cloud account's provider rejected the stored credential — the UI says
 * "reconnect" rather than the generic action-failed toast. */
export function isCloudAuthError(e: unknown): boolean {
  return e instanceof ApiError && e.status === 400 && e.type === "cloud_auth";
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

// ---------------------------------------------------------------------------
// Project model: Project → Repository → Component → Check/Link.
// Mirrors server/internal/domain/project_model.go and project_model_requests.go
// JSON tags exactly (Phase 1 HTTP contract). Every detected field is a
// Fact<T>: effective value = override ?? detected; "edited by you" = override
// present; PATCHing a field to `null` clears the override (revert to
// detected), an absent field is left untouched, and JSON.stringify already
// drops `undefined` keys, so the three patch spellings fall out for free.
// ---------------------------------------------------------------------------

export type Confidence = "exact" | "high" | "medium" | "low";

export interface SourceEvidence {
  path: string;
  line?: number;
  note?: string;
}

export interface Fact<T> {
  detected?: T;
  override?: T;
  confidence?: Confidence;
  evidence?: SourceEvidence[];
}

export type ComponentRole =
  | "frontend"
  | "backend"
  | "mobile"
  | "desktop"
  | "worker"
  | "library"
  | "infra"
  | "cli"
  | "other";

export const COMPONENT_ROLES: ComponentRole[] = [
  "frontend",
  "backend",
  "mobile",
  "desktop",
  "worker",
  "library",
  "infra",
  "cli",
  "other",
];

export interface StackItem {
  name: string;
  version?: string;
  evidence?: SourceEvidence;
}

export interface ComponentStack {
  languages?: StackItem[];
  frameworks?: StackItem[];
  libraries?: StackItem[];
  runtime?: StackItem;
  package_manager?: string;
  container?: string;
}

export type CommandPurpose =
  | "install"
  | "dev"
  | "build"
  | "test"
  | "lint"
  | "typecheck"
  | "format"
  | "e2e"
  | "migrate";

export const COMMAND_PURPOSES: CommandPurpose[] = [
  "install",
  "dev",
  "build",
  "test",
  "lint",
  "typecheck",
  "format",
  "e2e",
  "migrate",
];

export interface ComponentCommand {
  purpose: CommandPurpose;
  command: Fact<string>;
}

/** Read off the working copy (repository.DetectAppIdentity); never typed by a human. */
export interface AppIdentity {
  bundle_id: string;
  package_name: string;
}

/** Read off the working copy (repository.DetectBuildTargets); "" blocks a mobile release. */
export interface BuildTargets {
  xcode_scheme: string;
  gradle_module: string;
}

export interface MobileFacts {
  platform?: string;
  identity: AppIdentity;
  build_targets: BuildTargets;
}

/** nil/absent inherits the repository-level gate setting. */
export interface ComponentGates {
  coverage_enabled?: boolean;
  coverage_threshold?: number;
  mutation_enabled?: boolean;
  mutation_threshold?: number;
}

export type ComponentStatus = "active" | "dismissed";

export type DeliveryMode = "on_merge" | "dispatch" | "batch" | "none";

export const DELIVERY_MODES: DeliveryMode[] = ["on_merge", "dispatch", "batch", "none"];

export type DeliveryExecutor = "github_actions" | "vercel" | "local" | "store";

export const DELIVERY_EXECUTORS: DeliveryExecutor[] = ["github_actions", "vercel", "local", "store"];

export const DEFAULT_SOAK_MINUTES = 10;
export const MAX_SOAK_MINUTES = 120;
export const MAX_SMOKE_CHECKS = 20;

/** One read-only request sent to production after a deploy — only GET/HEAD
 * exist; a smoke check that could write would be a test against production. */
export interface SmokeCheck {
  method?: string;
  /** Relative to the environment's URL ("/api/health"), or an absolute http(s) URL. */
  path: string;
  /** Absent/0 means any 2xx/3xx. */
  expect_status?: number;
  /** Must appear in the response body, when set. */
  contains?: string;
}

export interface DeliveryVerify {
  /** How long production is watched after the deploy settles before a
   * verdict is asked for. */
  soak_minutes?: number;
  /** How many new runtime error groups are tolerated before the soak stops early. */
  max_new_errors?: number;
  smoke?: SmokeCheck[];
}

/** A component's delivery profile: WHAT a merge sets in motion and WHO
 * carries it out. Mirrors server/internal/domain/delivery.go's JSON tags
 * exactly. */
export interface ComponentDelivery {
  mode: DeliveryMode;
  executor?: DeliveryExecutor;
  /** The deploy workflow's file basename ("deploy.yml"). */
  workflow?: string;
  /** Names a batch release's tag; "{version}" is substituted. */
  tag_pattern?: string;
  /** Runs a batch release on this machine; "{version}" is substituted. */
  local_command?: string;
  verify: DeliveryVerify;
  /** Off: a bad release is written up for a human instead of rolled back on its own. */
  auto_rollback: boolean;
}

export interface Component {
  id: string;
  repository_id: string;
  path: string;
  name: Fact<string>;
  role: Fact<ComponentRole>;
  stack: Fact<ComponentStack>;
  commands: ComponentCommand[];
  mobile?: Fact<MobileFacts>;
  docs: RepositoryDocs;
  gates: ComponentGates;
  /** How a merge of this component reaches production; unconfirmed (no
   * override and detected confidence below high) means nothing deploys it.
   * Optional here, though the server always sends it, so pre-existing
   * Component fixtures elsewhere in the test suite need no update. */
  delivery?: Fact<ComponentDelivery>;
  status: ComponentStatus;
  manually_added: boolean;
  // A later scan (a push, not the first import) found this on its own; the
  // human keeps (reviewed: true) or dismisses it (status: "dismissed").
  needs_review: boolean;
  last_scan_id?: string;
  created_at: string;
  updated_at: string;
}

export type CheckPurpose =
  | "lint"
  | "typecheck"
  | "test"
  | "build"
  | "e2e"
  | "security"
  | "deploy"
  | "release"
  | "other";

export const CHECK_PURPOSES: CheckPurpose[] = [
  "lint",
  "typecheck",
  "test",
  "build",
  "e2e",
  "security",
  "deploy",
  "release",
  "other",
];

export type CheckGate = "required" | "info" | "off";

export const CHECK_GATES: CheckGate[] = ["required", "info", "off"];

/** One argv run without a shell inside `dir` (repo-relative). */
export interface LocalCommand {
  dir: string;
  argv: string[];
}

export interface CheckStep {
  name?: string;
  run?: string;
  uses?: string;
  working_directory?: string;
}

export type CheckSource = "ci" | "manual";
export type ModelStatus = "active" | "dismissed";

// Distinct from DeployEnv ("local"|"stage"|"preprod"|"prod", used by deploy
// targets): this is the CI workflow's own environment label, read off the job.
export type DeployEnvironment = "production" | "staging" | "preview" | "development";

/** One CI job mapped onto the component it verifies. */
export interface ComponentCheck {
  id: string;
  repository_id: string;
  component_id: string;
  source: CheckSource;
  workflow: string;
  workflow_name?: string;
  job_key: string;
  job_name?: string;
  purpose: Fact<CheckPurpose>;
  environment?: DeployEnvironment;
  triggers?: string[];
  path_filters?: string[];
  steps?: CheckStep[];
  local_commands: Fact<LocalCommand[]>;
  gate: Fact<CheckGate>;
  dispatchable: boolean;
  status: ModelStatus;
  missing: boolean;
  // A later scan added this REQUIRED check, which changes what every agent
  // must pass before hand-off; the human acknowledges (reviewed: true) or
  // makes it informative (gate: "info", reviewed: true).
  needs_review: boolean;
  created_at: string;
  updated_at: string;
}

export type ResourceKind =
  | "database"
  | "cache"
  | "queue"
  | "storage"
  | "search"
  | "api"
  | "auth"
  | "email"
  | "payments"
  | "ai"
  | "observability"
  | "other";

export const RESOURCE_KINDS: ResourceKind[] = [
  "database",
  "cache",
  "queue",
  "storage",
  "search",
  "api",
  "auth",
  "email",
  "payments",
  "ai",
  "observability",
  "other",
];

/** A workspace-wide node something connects to; IdentityKey dedupes it. */
export interface SystemResource {
  id: string;
  kind: ResourceKind;
  vendor: string;
  name: string;
  identity_key: string;
  details?: Record<string, string>;
  /** true once a person has renamed it — a later scan no longer overwrites the name. */
  name_locked?: boolean;
  created_at: string;
  updated_at: string;
}

export type LinkProtocol = "http" | "grpc" | "graphql" | "sql" | "redis" | "queue" | "sdk" | "package" | "other";

export const LINK_PROTOCOLS: LinkProtocol[] = [
  "http",
  "grpc",
  "graphql",
  "sql",
  "redis",
  "queue",
  "sdk",
  "package",
  "other",
];

export type LinkStatus = "suggested" | "confirmed" | "dismissed";

export const LINK_STATUSES: LinkStatus[] = ["suggested", "confirmed", "dismissed"];

export type LinkSource = "scan" | "user";

/**
 * One outgoing edge. Exactly one of to_component_id/to_resource_id is set
 * once resolved; an unresolved suggestion has neither and carries `hint`.
 */
export interface ComponentLink {
  id: string;
  repository_id: string;
  from_component_id: string;
  to_component_id?: string;
  to_resource_id?: string;
  protocol: LinkProtocol;
  detail?: string;
  env_vars?: string[];
  evidence?: SourceEvidence[];
  confidence: Confidence;
  reason?: string;
  hint?: string;
  status: LinkStatus;
  source: LinkSource;
  auto_confirmed: boolean;
  signal_key?: string;
  // What an unresolved URL pointed at. When an environment is bound whose URL
  // host/domain equals it, the link is re-matched to that component
  // automatically (auto_confirmed).
  target_host?: string;
  target_port?: number;
  missing: boolean;
  created_at: string;
  updated_at: string;
}

export type ScanTrigger = "import" | "manual" | "push" | "stale" | "migrate";

export type ScanStatus = "queued" | "running" | "succeeded" | "failed";

export type ScanStage =
  | "clone"
  | "inventory"
  | "shape"
  | "components"
  | "stack"
  | "checks"
  | "links"
  | "deploy"
  | "match";

/** One progress line the add-repository flow streams; summary is a finished
 * sentence of what the stage found. */
export interface ScanEvent {
  stage: ScanStage;
  done: boolean;
  summary?: string;
  at: string;
}

export interface ProjectScan {
  id: string;
  repository_id: string;
  trigger: ScanTrigger;
  status: ScanStatus;
  stage?: ScanStage;
  commit_sha?: string;
  events: ScanEvent[];
  // Never populated by the endpoints the UI calls (scans/latest, /v1/scans/:id) —
  // ScanResult is the parser's internal record, replayed for audit only.
  result?: unknown;
  review_count: number;
  error?: string;
  started_at: string;
  finished_at?: string;
}

export type RepoShape = "single" | "monorepo";

export type ProjectType = "empty" | "single_repo" | "monorepo" | "multi_repo";

export type ReviewKind = "role" | "link" | "environment" | "component" | "check";

/** Points at one medium-confidence value waiting for the human; the UI
 * renders it from the entity in the same payload. */
export interface ReviewItem {
  kind: ReviewKind;
  entity_id: string;
  repository_id: string;
  component_id: string;
  confidence: Confidence;
}

/** Display identity of a component that lives in another repository but is
 * the other end of one of this payload's links. */
export interface LinkedComponent {
  id: string;
  repository_id: string;
  repository_name: string;
  project_ids?: string[];
  path: string;
  name: string;
  role: ComponentRole;
}

/** GET /v1/repositories/:id/model. */
export interface RepositoryModel {
  repository: Repository;
  shape: RepoShape;
  components: Component[];
  checks: ComponentCheck[];
  links: ComponentLink[];
  incoming_links: ComponentLink[];
  resources: SystemResource[];
  linked_components: LinkedComponent[];
  environments: ComponentEnvironment[];
  review: ReviewItem[];
  latest_scan?: ProjectScan;
}

export interface ComponentSummary {
  id: string;
  path: string;
  name: string;
  role: ComponentRole;
  role_confidence?: Confidence;
  stack_summary: string;
  checks: number;
  required_checks: number;
}

export interface ScanSummary {
  id: string;
  status: ScanStatus;
  trigger: ScanTrigger;
  started_at: string;
  finished_at?: string;
}

export interface RepositorySummary {
  id: string;
  name: string;
  description: string;
  remote_url?: string;
  project_ids?: string[];
  shape: RepoShape;
  components: ComponentSummary[];
  review_count: number;
  last_scan?: ScanSummary;
  // Filled from stored health only, so the projects page never waits on a
  // cloud provider.
  environments: EnvironmentSummary[];
  git_warning?: string;
  updated_at: string;
}

export interface ProjectRef {
  id: string;
  name: string;
}

export interface ResourceRefView {
  id: string;
  kind: ResourceKind;
  name: string;
}

/** One component that has a non-dismissed link to a workspace resource. */
export interface ResourceUser {
  repository_id: string;
  repository_name: string;
  project_ids: string[];
  component_id: string;
  component_path: string;
}

/** GET /v1/resources' entry: a resource plus who links to it, for the
 * resource picker's "in this repo / project / elsewhere" sections and the
 * link panel's "used by" list. */
export interface WorkspaceResource {
  resource: SystemResource;
  users: ResourceUser[];
  projects: ProjectRef[];
  link_count: number;
}

/**
 * One card on the projects page and the header of the project page.
 * cross_projects/cross_links/shared_resources are empty when the project is
 * independent.
 */
export interface ProjectOverview {
  id: string;
  name: string;
  description: string;
  type: ProjectType;
  repositories: RepositorySummary[];
  review_count: number;
  cross_projects: ProjectRef[];
  cross_links: number;
  shared_resources: ResourceRefView[];
}

/** GET /v1/projects/overview. */
export interface ProjectsOverview {
  projects: ProjectOverview[];
  unassigned: RepositorySummary[];
}

/** GET /v1/projects/:projectId/overview. */
export interface ProjectDetail extends ProjectOverview {
  review: ReviewItem[];
}

// ---- Project model request bodies ----------------------------------------
// Patch bodies read `T | null | undefined`: undefined (key omitted — dropped
// by JSON.stringify) leaves the override untouched, null clears it (revert to
// detected), a value sets it. See the Fact<T> comment above.

export interface ComponentPatch {
  name?: string | null;
  role?: ComponentRole | null;
  commands?: Partial<Record<CommandPurpose, string | null>>;
  gates?: ComponentGates | null;
  docs?: RepositoryDocs | null;
  /** null clears the override and reverts to the detected profile. */
  delivery?: ComponentDelivery | null;
  status?: ComponentStatus;
  /** true acknowledges a component a later scan added (clears needs_review). */
  reviewed?: boolean;
}

export interface NewComponentRequest {
  path: string;
  name?: string;
  role: ComponentRole;
}

export interface CheckPatch {
  purpose?: CheckPurpose | null;
  gate?: CheckGate | null;
  local_commands?: LocalCommand[] | null;
  status?: ModelStatus;
  /** true acknowledges a REQUIRED check a later scan added (clears needs_review). */
  reviewed?: boolean;
}

/** component_id is redundant with the /v1/components/:componentId/checks URL;
 * api.createCheck fills it in so callers never pass it twice. */
export interface NewCheckRequest {
  component_id: string;
  name: string;
  purpose: CheckPurpose;
  local_commands: LocalCommand[];
  gate: CheckGate;
}

/** A resource the human picked or typed; the server turns it into a
 * SystemResource with a user-scoped identity key. */
export interface ResourceRef {
  kind: ResourceKind;
  vendor?: string;
  name: string;
}

export interface LinkPatch {
  status?: LinkStatus;
  to_component_id?: string;
  to_resource_id?: string;
  to_resource?: ResourceRef;
  protocol?: LinkProtocol;
}

export interface NewLinkRequest {
  from_component_id: string;
  to_component_id?: string;
  to_resource?: ResourceRef;
  protocol: LinkProtocol;
  detail?: string;
}

// ---------------------------------------------------------------------------
// Cloud & runtime (Phase 2): connected provider accounts, the resources they
// can see, and per-component-environment deployments/logs/errors read
// through them. Mirrors server/internal/domain/cloud.go's JSON tags exactly.
// ---------------------------------------------------------------------------

export type CloudProviderKind = "vercel" | "gcp" | "aws";

export const CLOUD_PROVIDERS: CloudProviderKind[] = ["vercel", "gcp", "aws"];

export type CloudAccountStatus = "ok" | "error" | "unverified";

/** One connected provider login. The credential itself never round-trips;
 * `meta` carries the non-secret identity the provider reported (team,
 * project id, account id, region, client email). */
export interface CloudAccount {
  id: string;
  provider: CloudProviderKind;
  label: string;
  meta: Record<string, string>;
  status: CloudAccountStatus;
  status_detail?: string;
  verified_at?: string;
  created_at: string;
  updated_at: string;
}

/** fields: vercel {token, team_id?} · gcp {service_account_json} · aws
 * {access_key_id, secret_access_key, session_token?, region}. */
export interface SaveCloudAccountRequest {
  provider: CloudProviderKind;
  label?: string;
  fields: Record<string, string>;
}

export type CloudResourceKind =
  | "vercel_project"
  | "cloud_run_service"
  | "cloud_run_job"
  | "app_engine_service"
  | "cloud_function"
  | "gke_workload"
  | "ecs_service"
  | "lambda_function"
  | "app_runner_service";

/** One deployable thing inside an account. `id` is the provider-native
 * identifier (prj_…, a Cloud Run resource name, an ARN). */
export interface CloudResourceRef {
  kind: CloudResourceKind;
  id: string;
  name: string;
  region?: string;
  extra?: Record<string, string>;
}

/** One row of an account's resource listing. */
export interface CloudResource {
  account_id: string;
  provider: CloudProviderKind;
  ref: CloudResourceRef;
  url?: string;
  domains?: string[];
  labels?: Record<string, string>;
}

export type CloudResourceStatus = "healthy" | "deploying" | "degraded" | "failed" | "unknown";

export interface KeyValue {
  label: string;
  value: string;
}

export interface CloudResourceDetail extends CloudResource {
  status: CloudResourceStatus;
  status_detail?: string;
  revision?: string;
  console_url?: string;
  latest_deployment?: CloudDeployment;
  facts?: KeyValue[];
}

export type CloudDeploymentStatus = "ready" | "building" | "error" | "canceled" | "unknown";

export interface CloudDeployment {
  id: string;
  status: CloudDeploymentStatus;
  environment?: DeployEnvironment;
  commit_sha?: string;
  commit_message?: string;
  branch?: string;
  url?: string;
  creator?: string;
  created_at: string;
  ready_at?: string;
  inspect_url?: string;
}

export type LogSeverity = "debug" | "info" | "warning" | "error" | "critical";

export const LOG_SEVERITIES: LogSeverity[] = ["debug", "info", "warning", "error", "critical"];

export interface RuntimeLogQuery {
  since?: string;
  until?: string;
  min_severity?: LogSeverity;
  text?: string;
  limit?: number;
  cursor?: string;
}

export interface RuntimeLogEntry {
  timestamp: string;
  severity: LogSeverity;
  message: string;
  source?: string;
  method?: string;
  path?: string;
  status_code?: number;
  trace_id?: string;
  fields?: Record<string, string>;
}

export interface RuntimeLogPage {
  entries: RuntimeLogEntry[];
  next_cursor?: string;
  /** The provider capped the window; the list is not complete. */
  truncated?: boolean;
}

/** One recurring error. Providers with native grouping (GCP Error Reporting)
 * fill it directly; otherwise error-level log lines are grouped by a
 * normalised message fingerprint. */
export interface RuntimeErrorGroup {
  fingerprint: string;
  message: string;
  count: number;
  first_seen: string;
  last_seen: string;
  sample?: string;
  source?: string;
  /** The group's first occurrence falls inside the queried window — "started
   * with this deploy". */
  new: boolean;
  external_url?: string;
}

/**
 * Binds one component's environment to where it runs. Provider "" with only
 * url/health_url is a custom environment the platform can probe but not read
 * logs from.
 */
export interface ComponentEnvironment {
  id: string;
  repository_id: string;
  component_id: string;
  environment: DeployEnvironment;
  provider?: CloudProviderKind;
  account_id?: string;
  resource?: CloudResourceRef;
  url?: string;
  health_url?: string;
  status: LinkStatus;
  source: LinkSource;
  confidence: Confidence;
  reason?: string;
  /** The matcher bound this from an exact signal (a unique service name, a
   * project id) without asking. */
  auto_confirmed: boolean;
  /** The resources an ambiguous signal could mean; the human picks one. Empty
   * once confirmed. */
  candidates?: CloudResource[];
  signal_key?: string;
  /** The last background probe's summary; absent until one ran. */
  health?: EnvironmentHealth;
  created_at: string;
  updated_at: string;
}

export interface SaveEnvironmentRequest {
  account_id?: string;
  resource?: CloudResourceRef;
  url?: string;
  health_url?: string;
}

/** Choosing one of `ComponentEnvironment.candidates` sends its account_id +
 * resource.ref alongside status "confirmed". */
export interface EnvironmentPatch {
  status?: "confirmed" | "dismissed" | "suggested";
  account_id?: string;
  resource?: CloudResourceRef;
}

/** GET …/environments/:envId/overview — the live picture the Deploy &
 * Runtime tab opens on. */
export type EnvironmentUnavailableCode = "not_connected" | "cloud_auth" | "provider_error";

export interface EnvironmentRuntime {
  environment: ComponentEnvironment;
  detail?: CloudResourceDetail;
  deployments: CloudDeployment[];
  errors: RuntimeErrorGroup[];
  unavailable?: string;
  /** Why `unavailable` is set, so the UI can act on it instead of pattern
   * matching the message text. */
  unavailable_code?: EnvironmentUnavailableCode;
}

/** Refreshed by a background sweep so list views never call a provider on
 * load. */
export interface EnvironmentHealth {
  status: CloudResourceStatus;
  error_count_24h: number;
  last_deploy_at?: string;
  checked_at: string;
  detail?: string;
}

/** RepositorySummary.environments[] — cheap, from stored health only. */
export interface EnvironmentSummary {
  id: string;
  component_id: string;
  environment: DeployEnvironment;
  provider?: CloudProviderKind;
  resource_name?: string;
  url?: string;
  status: LinkStatus;
  health?: CloudResourceStatus;
  error_count_24h: number;
}

// ---------------------------------------------------------------------------
// Releases: one shipment of one component — the merges it
// carries, how it was deployed, what production looked like afterwards, and
// the verdict. A task reaches `released` only through its release's verdict,
// never through a deploy job's colour alone. Mirrors
// server/internal/domain/release.go and deploy_watch.go's JSON tags exactly.
// ---------------------------------------------------------------------------

export type ReleaseStatus =
  | "draft"
  | "pending"
  | "deploying"
  | "verifying"
  | "awaiting_verdict"
  | "rolling_back"
  | "released"
  | "rolled_back"
  | "failed"
  | "superseded";

/** Who performed a release action; an agent is refused what only a human may
 * confirm (a rollback with auto_rollback off). */
export type ReleaseActor = "agent" | "human" | "system";

export interface ReleaseTaskRef {
  id: string;
  key?: string;
  title?: string;
  task_type?: TaskType;
  column?: TaskColumn;
  merge_commit_sha?: string;
}

export interface HealthSample {
  at: string;
  status?: number;
  ok: boolean;
  latency_ms?: number;
  error?: string;
}

export interface SmokeResult {
  check: SmokeCheck;
  url: string;
  at: string;
  status?: number;
  ok: boolean;
  latency_ms?: number;
  error?: string;
}

/** The evidence gathered while a release was verified. */
export interface ReleaseChecks {
  /** The bound production environment the runtime reads came from; absent
   * when the component has none (health/smoke only). */
  environment_id?: string;
  base_url?: string;
  health_url?: string;
  health?: HealthSample[];
  smoke?: SmokeResult[];
  /** Runtime error groups whose first occurrence is after the deploy settled. */
  new_errors?: RuntimeErrorGroup[];
  /** Gaps the verifier hit ("no health URL", "logs not readable"), so a
   * clean result is never mistaken for a checked one. */
  notes?: string[];
  /** Why the soak ended before its window did. */
  early_stop?: string;
}

export type RollbackReason = "deploy_failed" | "verify_failed" | "health_incident" | "manual";

export const ROLLBACK_REASONS: RollbackReason[] = ["deploy_failed", "verify_failed", "health_incident", "manual"];

/** HOW a release was undone: redeploying the previous good release
 * (dispatch) or a pushed revert that redeploys on merge (on_merge). */
export type RollbackMechanism = "workflow_dispatch" | "revert_push";

export interface ReleaseRollback {
  reason: RollbackReason;
  note?: string;
  mechanism?: RollbackMechanism;
  /** The commit on the default branch that undoes the release's merges —
   * pushed in every mode, so the next release cannot ship the bad change again. */
  revert_sha?: string;
  /** What was redeployed: the previous release's tag or the revert commit. */
  restored_ref?: string;
  run_url?: string;
  /** What no mechanism can undo (migrations, flags, CDN) — from the tasks'
   * own rollback plans; the agent performs or reports each. */
  manual_steps?: string[];
  actor?: string;
  started_at: string;
  detail?: string;
}

export type DeployWatchState = "pending" | "success" | "failure" | "no_signal" | "unknown";

/** One job inside the Actions run that carried the deploy. */
export interface DeployWatchJob {
  id: number;
  name: string;
  status: string;
  conclusion: string;
  url?: string;
}

/** One task-commit's deploy-watch answer, carried on a release's `deploy` field. */
export interface DeployWatchStatus {
  task_id: string;
  task_key?: string;
  repository_id: string;
  env: string;
  merge_commit_sha: string;
  state: DeployWatchState;
  signal: string;
  detail?: string;
  run_id?: number;
  run_url?: string;
  failed_job?: DeployWatchJob;
  contexts?: string[];
  health_url?: string;
  logs_url?: string;
  auto_rollback: boolean;
  health_window_until?: string;
  checked_at: string;
}

/** A batch release's build-and-publish command run on this machine (executor
 * `local`). */
export interface ReleaseLocalRun {
  argv: string[];
  log_path?: string;
  exit_code?: number;
  /** The last ~120 lines of the command's combined output. */
  tail?: string;
  started_at: string;
  finished_at?: string;
  error?: string;
}

/** One platform's store build of a batch release (executor `store`): the
 * deploy is done when the store's internal channel shows a build other than
 * the baseline it had before. */
export interface ReleaseStoreBuild {
  platform: string;
  engine?: string;
  baseline_build?: string;
  build?: string;
  error?: string;
}

/** One shipment of one component: the merge commits it carries, how it was
 * deployed, what production looked like afterwards and what was decided. */
export interface Release {
  id: string;
  repository_id: string;
  component_id?: string;
  version: string;
  mode: DeliveryMode;
  executor?: DeliveryExecutor;
  status: ReleaseStatus;
  /** The commit this release puts in production: the task's merge commit, or
   * the default branch head a batch was cut at. */
  commit_sha?: string;
  tag?: string;
  notes?: string;
  /** The delivery profile the release was opened under, frozen so a later
   * edit cannot change what an in-flight release is judged by. */
  profile: ComponentDelivery;
  deploy?: DeployWatchStatus;
  local_run?: ReleaseLocalRun;
  store_builds?: ReleaseStoreBuild[];
  checks: ReleaseChecks;
  /** The release engineer's (or a human's) closing note. */
  verdict?: string;
  rollback?: ReleaseRollback;
  /** Set with status `failed`. */
  failure_reason?: string;
  card_task_id?: string;
  tasks: ReleaseTaskRef[];
  created_at: string;
  updated_at: string;
  deploy_started_at?: string;
  deployed_at?: string;
  verify_until?: string;
  finished_at?: string;
  /** Set when a human cut this batch release. */
  cut_at?: string;
}

/** GET .../cut-preview: what cutting a batch component's draft release right
 * now would produce, before a human commits to a version. */
export interface ReleaseCutPreview {
  suggested_version: string;
  previous_version?: string;
  /** The tag `suggested_version` would get, from the profile's tag pattern. */
  tag?: string;
  commit_sha: string;
  /** Generated release notes markdown. */
  notes: string;
  tasks: ReleaseTaskRef[];
}

export interface ReleaseCutRequest {
  version: string;
  notes: string;
}

/** What merging a task set in motion; carried on the merge result so the
 * release engineer knows its next step without another call. */
export interface ReleaseOpening {
  mode: DeliveryMode;
  release_id?: string;
  status?: ReleaseStatus;
  /** True when the merge was the whole release (mode none). */
  released?: boolean;
  /** True when the component's delivery profile has not been confirmed:
   * nothing deploys and the task waits in done. */
  unconfirmed?: boolean;
  next: string;
}

// ---------------------------------------------------------------------------
// Maps & review (Phase 3): the project architecture map and the all-projects
// workspace map. Mirrors server/internal/domain/project_map.go's JSON tags
// exactly. `GET /v1/projects/map` is registered before `/v1/projects/:id`.
// ---------------------------------------------------------------------------

/** The column a node sits in: callers on the left, what they call to the
 * right. "library" is shown only when the map's library toggle is on. */
export type MapTier = "client" | "service" | "library" | "data" | "external";

export type MapNodeKind = "component" | "resource";

/** A component or a resource on one project's architecture map. `foreign`
 * marks a component of another project, drawn only because one of this
 * project's edges reaches it. */
export interface MapNode {
  id: string;
  kind: MapNodeKind;
  tier: MapTier;
  label: string;
  role?: ComponentRole;
  resource_kind?: ResourceKind;
  vendor?: string;
  component_id?: string;
  resource_id?: string;
  repository_id?: string;
  repository_name?: string;
  path?: string;
  project_ids?: string[];
  foreign: boolean;
  stack_summary?: string;
  provider?: CloudProviderKind;
  health?: CloudResourceStatus;
  error_count_24h?: number;
  /** The other projects linking the same resource. */
  shared_with?: ProjectRef[];
}

export interface MapEdge {
  id: string;
  link_id: string;
  from: string;
  to: string;
  protocol: LinkProtocol;
  detail?: string;
  status: LinkStatus;
  source: LinkSource;
  env_vars?: string[];
  /** Set when the edge leaves the project the map is for. */
  cross_project: boolean;
}

/** GET /v1/projects/:projectId/map. */
export interface ProjectMap {
  project: ProjectRef;
  nodes: MapNode[];
  edges: MapEdge[];
}

export interface WorkspaceMapRepository {
  id: string;
  name: string;
  shape: RepoShape;
  components: ComponentSummary[];
}

export interface WorkspaceMapProject {
  id: string;
  name: string;
  type: ProjectType;
  repositories: WorkspaceMapRepository[];
}

/** Aggregates every link between two projects; independent projects simply
 * have none. */
export interface WorkspaceMapEdge {
  from_project_id: string;
  to_project_id: string;
  links: number;
  suggested: number;
  protocols: LinkProtocol[];
  examples: string[];
}

export interface WorkspaceSharedResource {
  resource: ResourceRefView;
  project_ids: string[];
}

/** GET /v1/projects/map. */
export interface WorkspaceMap {
  projects: WorkspaceMapProject[];
  unassigned: WorkspaceMapRepository[];
  edges: WorkspaceMapEdge[];
  shared_resources: WorkspaceSharedResource[];
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

  getCatalogStatus: () => request<{ configured: boolean; state?: CatalogSyncState }>("/v1/catalog/status"),

  syncCatalog: () =>
    request<{ result: CatalogSyncResult; state: CatalogSyncState }>("/v1/catalog/sync", { method: "POST" }),

  listCatalogPending: () => request<{ items: CatalogPending[]; count: number }>("/v1/catalog/pending"),

  dismissCatalogPending: (id: string) =>
    request<void>(`/v1/catalog/pending/${id}`, { method: "DELETE" }),

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

  openRepository: (rootPath: string, description?: string, projectIds?: string[]) =>
    request<Repository>("/v1/repositories/open", {
      method: "POST",
      body: JSON.stringify({ root_path: rootPath, description, project_ids: projectIds }),
    }),

  openProject: (rootPath: string, description: string) => api.openRepository(rootPath, description),

  createRepository: (
    name: string,
    parentDir: string,
    description?: string,
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
      docs?: RepositoryDocs;
      release_engine?: ReleaseEngine;
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

  createWorkflowSetupTask: (id: string) =>
    request<BoardTask>(`/v1/repositories/${id}/pipeline/setup-task`, { method: "POST" }),

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
    request<AgentSubscriptionsResponse>(`/v1/agents/${agentId}/subscriptions`),

  // Detailed form: each column may filter which task types wake the agent
  // (null = every type). Superset of the legacy column_slugs-only PUT.
  setAgentSubscriptions: (agentId: string, subscriptions: AgentColumnSubscription[]) =>
    request<void>(`/v1/agents/${agentId}/subscriptions`, {
      method: "PUT",
      body: JSON.stringify({ subscriptions }),
    }),

  listRoles: () => request<{ roles: Role[] }>("/v1/roles"),

  createRole: (data: CreateRoleInput) =>
    request<Role>("/v1/roles", { method: "POST", body: JSON.stringify(data) }),

  updateRole: (id: string, data: UpdateRoleInput) =>
    request<Role>(`/v1/roles/${id}`, { method: "PUT", body: JSON.stringify(data) }),

  deleteRole: (id: string) => request<void>(`/v1/roles/${id}`, { method: "DELETE" }),

  // 422 (missing_tools, saved:false) is a normal outcome — the caller opens a
  // confirm-and-grant dialog — so this bypasses request()'s throw-on-non-2xx.
  setRoleAssignments: async (id: string, req: SetRoleAssignmentsInput) => {
    const res = await fetchWithTimeout(
      apiUrl(`/v1/roles/${id}/assignments`),
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
    return (await res.json()) as RoleAssignmentsResult;
  },

  getAgentRoles: (agentId: string) => request<{ roles: AgentRoleRef[] }>(`/v1/agents/${agentId}/roles`),

  setAgentRoles: async (agentId: string, req: SetAgentRolesInput) => {
    const res = await fetchWithTimeout(
      apiUrl(`/v1/agents/${agentId}/roles`),
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
    return (await res.json()) as AgentRolesResult;
  },

  listRolePurposes: () => request<{ purposes: RolePurposeAssignment[] }>("/v1/role-purposes"),

  setRolePurpose: (purpose: RolePurposeKey, roleId: string | null) =>
    request<void>(`/v1/role-purposes/${purpose}`, {
      method: "PUT",
      body: JSON.stringify({ role_id: roleId }),
    }),

  listTaskTypes: () => request<{ task_types: TaskTypeDef[] }>("/v1/task-types"),

  createTaskType: (data: CreateTaskTypeInput) =>
    request<TaskTypeDef>("/v1/task-types", { method: "POST", body: JSON.stringify(data) }),

  updateTaskType: (key: string, data: UpdateTaskTypeInput) =>
    request<TaskTypeDef>(`/v1/task-types/${key}`, { method: "PUT", body: JSON.stringify(data) }),

  deleteTaskType: (key: string) => request<void>(`/v1/task-types/${key}`, { method: "DELETE" }),

  getTaskTypeWorkflow: (key: string) => request<TaskTypeWorkflow>(`/v1/task-types/${key}/workflow`),

  // 422 with `problems` is a normal outcome (inline per-stage validation),
  // not an error — bypasses request()'s throw-on-non-2xx like the role/agent
  // assignment writes above.
  updateTaskTypeWorkflow: async (key: string, stages: WorkflowStage[]) => {
    const res = await fetchWithTimeout(
      apiUrl(`/v1/task-types/${key}/workflow`),
      {
        method: "PUT",
        headers: authHeaders({ "Content-Type": "application/json" }),
        body: JSON.stringify({ stages }),
      },
      WRITE_TIMEOUT_MS,
    );
    if (!res.ok && res.status !== 422) {
      throw await apiErrorFrom(res);
    }
    return (await res.json()) as UpdateWorkflowResult;
  },

  listWorkflows: () => request<{ workflows: TaskTypeWorkflow[] }>("/v1/workflows"),

  listWorkflowBehaviours: () => request<WorkflowBehavioursResponse>("/v1/workflow/behaviours"),

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

  // ---- Project model ------------------------------------------------------

  getProjectsOverview: () => request<ProjectsOverview>("/v1/projects/overview"),

  getProjectOverview: (projectId: string) => request<ProjectDetail>(`/v1/projects/${projectId}/overview`),

  // Registered server-side before /v1/projects/:projectId, so "map" is never
  // read back as a project id.
  getWorkspaceMap: () => request<WorkspaceMap>("/v1/projects/map"),

  getProjectMap: (projectId: string) => request<ProjectMap>(`/v1/projects/${projectId}/map`),

  getRepositoryModel: (repositoryId: string) => request<RepositoryModel>(`/v1/repositories/${repositoryId}/model`),

  // A scan already running on this repository answers 409, not an error the
  // caller should surface — the running scan is exactly what the add-repository
  // flow wants to poll, so this folds that case back into a normal result.
  startRepositoryScan: async (repositoryId: string): Promise<ProjectScan> => {
    try {
      const res = await request<{ scan: ProjectScan }>(`/v1/repositories/${repositoryId}/scans`, {
        method: "POST",
      });
      return res.scan;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        const latest = await api.getLatestRepositoryScan(repositoryId);
        if (latest.scan) return latest.scan;
      }
      throw e;
    }
  },

  getLatestRepositoryScan: (repositoryId: string) =>
    request<{ scan: ProjectScan | null }>(`/v1/repositories/${repositoryId}/scans/latest`),

  getScan: (scanId: string) => request<{ scan: ProjectScan }>(`/v1/scans/${scanId}`),

  createComponent: (repositoryId: string, body: NewComponentRequest) =>
    request<Component>(`/v1/repositories/${repositoryId}/components`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateComponent: (componentId: string, patch: ComponentPatch) =>
    request<Component>(`/v1/components/${componentId}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  createCheck: (componentId: string, body: Omit<NewCheckRequest, "component_id">) =>
    request<ComponentCheck>(`/v1/components/${componentId}/checks`, {
      method: "POST",
      body: JSON.stringify({ component_id: componentId, ...body }),
    }),

  updateCheck: (checkId: string, patch: CheckPatch) =>
    request<ComponentCheck>(`/v1/checks/${checkId}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  deleteCheck: (checkId: string) => request<void>(`/v1/checks/${checkId}`, { method: "DELETE" }),

  createLink: (body: NewLinkRequest) =>
    request<ComponentLink>("/v1/links", { method: "POST", body: JSON.stringify(body) }),

  updateLink: (linkId: string, patch: LinkPatch) =>
    request<ComponentLink>(`/v1/links/${linkId}`, { method: "PATCH", body: JSON.stringify(patch) }),

  deleteLink: (linkId: string) => request<void>(`/v1/links/${linkId}`, { method: "DELETE" }),

  listWorkspaceResources: () => request<{ resources: WorkspaceResource[] }>("/v1/resources"),

  renameResource: (resourceId: string, name: string) =>
    request<SystemResource>(`/v1/resources/${resourceId}`, { method: "PATCH", body: JSON.stringify({ name }) }),

  // Moves every link of resourceId onto intoResourceId and deletes resourceId;
  // future scans keep resolving its signals to the target.
  mergeResource: (resourceId: string, intoResourceId: string) =>
    request<SystemResource>(`/v1/resources/${resourceId}/merge`, {
      method: "POST",
      body: JSON.stringify({ into_resource_id: intoResourceId }),
    }),

  // Splits linkIds (which must currently point at resourceId) onto a fresh
  // copy of it, confirmed.
  splitResource: (resourceId: string, linkIds: string[]) =>
    request<SystemResource>(`/v1/resources/${resourceId}/split`, {
      method: "POST",
      body: JSON.stringify({ link_ids: linkIds }),
    }),

  // ---- Cloud accounts & environments (Phase 2) ---------------------------

  listCloudAccounts: () => request<{ accounts: CloudAccount[] }>("/v1/cloud-accounts"),

  // The server verifies with the provider before storing; a rejected
  // credential is a 400 {error, code:"cloud_auth"} — see isCloudAuthError.
  createCloudAccount: (data: SaveCloudAccountRequest) =>
    request<CloudAccount>("/v1/cloud-accounts", { method: "POST", body: JSON.stringify(data) }),

  updateCloudAccount: (id: string, data: { label?: string; fields?: Record<string, string> }) =>
    request<CloudAccount>(`/v1/cloud-accounts/${id}`, { method: "PATCH", body: JSON.stringify(data) }),

  verifyCloudAccount: (id: string) => request<CloudAccount>(`/v1/cloud-accounts/${id}/verify`, { method: "POST" }),

  deleteCloudAccount: (id: string) => request<void>(`/v1/cloud-accounts/${id}`, { method: "DELETE" }),

  listCloudResources: (accountId: string, opts: { refresh?: boolean } = {}) =>
    request<{ resources: CloudResource[] }>(
      `/v1/cloud-accounts/${accountId}/resources${opts.refresh ? "?refresh=1" : ""}`,
    ),

  bindEnvironment: (componentId: string, env: DeployEnvironment, body: SaveEnvironmentRequest) =>
    request<ComponentEnvironment>(`/v1/components/${componentId}/environments/${env}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  patchEnvironment: (envId: string, patch: EnvironmentPatch) =>
    request<ComponentEnvironment>(`/v1/environments/${envId}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  deleteEnvironment: (envId: string) => request<void>(`/v1/environments/${envId}`, { method: "DELETE" }),

  getEnvironmentOverview: (envId: string) => request<EnvironmentRuntime>(`/v1/environments/${envId}/overview`),

  getEnvironmentLogs: (envId: string, query: RuntimeLogQuery = {}) => {
    const qs = new URLSearchParams();
    if (query.since) qs.set("since", query.since);
    if (query.until) qs.set("until", query.until);
    if (query.min_severity) qs.set("min_severity", query.min_severity);
    if (query.text) qs.set("q", query.text);
    if (query.limit) qs.set("limit", String(query.limit));
    if (query.cursor) qs.set("cursor", query.cursor);
    const suffix = qs.toString();
    return request<RuntimeLogPage>(`/v1/environments/${envId}/logs${suffix ? `?${suffix}` : ""}`);
  },

  getEnvironmentErrors: (envId: string, opts: { since?: string } = {}) =>
    request<{ errors: RuntimeErrorGroup[] }>(
      `/v1/environments/${envId}/errors${opts.since ? `?since=${encodeURIComponent(opts.since)}` : ""}`,
    ),

  getEnvironmentDeployments: (envId: string, opts: { limit?: number } = {}) =>
    request<{ deployments: CloudDeployment[] }>(
      `/v1/environments/${envId}/deployments${opts.limit ? `?limit=${opts.limit}` : ""}`,
    ),

  createErrorTask: (envId: string, group: RuntimeErrorGroup) =>
    request<BoardTask>(`/v1/environments/${envId}/errors/task`, {
      method: "POST",
      body: JSON.stringify(group),
    }),

  // ---- Releases & delivery --------------------------------------

  listReleases: (repositoryId: string, opts: { componentId?: string; taskId?: string; limit?: number } = {}) => {
    const qs = new URLSearchParams();
    if (opts.componentId) qs.set("component_id", opts.componentId);
    if (opts.taskId) qs.set("task_id", opts.taskId);
    if (opts.limit) qs.set("limit", String(opts.limit));
    const suffix = qs.toString();
    return request<{ releases: Release[] }>(`/v1/repositories/${repositoryId}/releases${suffix ? `?${suffix}` : ""}`);
  },

  getRelease: (releaseId: string) => request<Release>(`/v1/releases/${releaseId}`),

  getReleaseCutPreview: (releaseId: string) => request<ReleaseCutPreview>(`/v1/releases/${releaseId}/cut-preview`),

  cutRelease: (releaseId: string, confirm: string, version: string, notes: string) =>
    request<Release>(`/v1/releases/${releaseId}/cut`, {
      method: "POST",
      body: JSON.stringify({ confirm, version, notes }),
    }),

  deployRelease: (releaseId: string, confirm: string) =>
    request<Release>(`/v1/releases/${releaseId}/deploy`, { method: "POST", body: JSON.stringify({ confirm }) }),

  finishRelease: (releaseId: string, confirm: string, note: string) =>
    request<Release>(`/v1/releases/${releaseId}/finish`, {
      method: "POST",
      body: JSON.stringify({ confirm, note }),
    }),

  rollbackRelease: (releaseId: string, confirm: string, note: string) =>
    request<Release>(`/v1/releases/${releaseId}/rollback`, {
      method: "POST",
      body: JSON.stringify({ confirm, note }),
    }),

  // PATCHes the component with only {delivery}: a value sets the override, null clears it.
  updateComponentDelivery: (componentId: string, delivery: ComponentDelivery | null) =>
    request<Component>(`/v1/components/${componentId}`, {
      method: "PATCH",
      body: JSON.stringify({ delivery }),
    }),
};
