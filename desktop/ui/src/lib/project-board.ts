import { AlertTriangle, CheckCircle2, CircleDashed, Clock, MinusCircle, XCircle, type LucideIcon } from "lucide-react";
import type {
  BoardTask,
  PipelineGateReason,
  Repository,
  TaskPipeline,
  TaskPriority,
  TaskType,
  TaskTypeDef,
} from "@/api";
import { tStatic } from "@/hooks/useI18n";

// Cache keys for the payloads the board and the backlog both render (see
// hooks/useCachedState). Shared so a move made on one page is already on
// screen when the other opens.
export const CACHE_TASKS = "board.tasks";
export const CACHE_REPOS = "board.repositories";
export const CACHE_PROJECTS = "board.initiativeProjects";
export const CACHE_CONFIG = "board.config";
export const CACHE_AGENTS = "board.agents";
export const CACHE_WORKFLOWS = "board.workflows";

/** Which slice of the workspace the board and backlog show: every task, the
 * tasks that belong to no project, or one project's id. */
export type ProjectScope = string;
export const PROJECT_SCOPE_ALL = "all";
export const PROJECT_SCOPE_NONE = "none";

type ScopedTask = Pick<BoardTask, "repository_id" | "initiative_project_id">;
type ScopedRepository = Pick<Repository, "id" | "project_ids">;

export function isProjectScope(scope: ProjectScope): boolean {
  return scope !== PROJECT_SCOPE_ALL && scope !== PROJECT_SCOPE_NONE;
}

function repositoryProjectIndex(repositories: readonly ScopedRepository[]): Map<string, readonly string[]> {
  return new Map(repositories.map((repo) => [repo.id, repo.project_ids ?? []]));
}

// A task's own initiative project is an explicit choice and wins outright; only
// a task that never picked one inherits every project its repository is in.
function taskProjectIds(task: ScopedTask, index: Map<string, readonly string[]>): readonly string[] {
  if (task.initiative_project_id) return [task.initiative_project_id];
  return index.get(task.repository_id) ?? [];
}

export function taskBelongsToProject(
  task: ScopedTask,
  projectId: string,
  repositories: readonly ScopedRepository[],
): boolean {
  return taskProjectIds(task, repositoryProjectIndex(repositories)).includes(projectId);
}

export function taskHasNoProject(task: ScopedTask, repositories: readonly ScopedRepository[]): boolean {
  return taskProjectIds(task, repositoryProjectIndex(repositories)).length === 0;
}

export function filterTasksByScope<T extends ScopedTask>(
  tasks: T[],
  scope: ProjectScope,
  repositories: readonly ScopedRepository[],
): T[] {
  if (scope === PROJECT_SCOPE_ALL) return tasks;
  const index = repositoryProjectIndex(repositories);
  if (scope === PROJECT_SCOPE_NONE) return tasks.filter((task) => taskProjectIds(task, index).length === 0);
  return tasks.filter((task) => taskProjectIds(task, index).includes(scope));
}

/**
 * The scope to switch to so `task` is on screen, or null when the current one
 * already shows it: the task's single project, else every project.
 */
export function scopeShowingTask(
  task: ScopedTask,
  scope: ProjectScope,
  repositories: readonly ScopedRepository[],
): ProjectScope | null {
  const ids = taskProjectIds(task, repositoryProjectIndex(repositories));
  if (scope === PROJECT_SCOPE_ALL) return null;
  if (scope === PROJECT_SCOPE_NONE ? ids.length === 0 : ids.includes(scope)) return null;
  return ids.length === 1 ? ids[0] : PROJECT_SCOPE_ALL;
}

export interface ProjectScopeCounts {
  all: number;
  none: number;
  byProject: Record<string, number>;
}

export function projectScopeCounts(
  tasks: readonly ScopedTask[],
  repositories: readonly ScopedRepository[],
): ProjectScopeCounts {
  const index = repositoryProjectIndex(repositories);
  const byProject: Record<string, number> = {};
  let none = 0;
  for (const task of tasks) {
    const ids = taskProjectIds(task, index);
    if (ids.length === 0) none += 1;
    for (const id of new Set(ids)) byProject[id] = (byProject[id] ?? 0) + 1;
  }
  return { all: tasks.length, none, byProject };
}

export interface TaskCreateDefaults<R> {
  repositories: R[];
  repositoryId: string;
  initiativeProjectId?: string;
}

/**
 * What the create-task dialog opens with while the page is scoped: the
 * project preselected, and its repositories first (and preselected) — the
 * others stay pickable, since a project's work can land in a repository it
 * does not own yet. Anything but a project scope leaves the defaults alone.
 */
export function taskCreateDefaults<R extends ScopedRepository>(
  repositories: R[],
  scope: ProjectScope,
  fallbackRepositoryId: string,
): TaskCreateDefaults<R> {
  if (!isProjectScope(scope)) return { repositories, repositoryId: fallbackRepositoryId };
  const own = repositories.filter((repo) => repo.project_ids?.includes(scope));
  if (own.length === 0) {
    return { repositories, repositoryId: fallbackRepositoryId, initiativeProjectId: scope };
  }
  const rest = repositories.filter((repo) => !repo.project_ids?.includes(scope));
  return { repositories: [...own, ...rest], repositoryId: own[0].id, initiativeProjectId: scope };
}

/**
 * Replaces a task list with a freshly fetched one while keeping the object (and
 * array) identity of everything that did not actually change.
 *
 * The board refetches on a timer so an agent's move shows up without a reload.
 * Handing React a brand-new object for every card on every tick would re-run
 * everything keyed on task identity — most visibly the open detail drawer,
 * which reloads its comments, runs and criteria whenever its `task` prop
 * changes. Unchanged cards keep their previous object, so a poll that found
 * nothing new costs nothing.
 */
export function mergeTaskList(prev: BoardTask[], next: BoardTask[]): BoardTask[] {
  const previousByID = new Map(prev.map((task) => [task.id, task]));
  let changed = prev.length !== next.length;
  const merged = next.map((task, index) => {
    const before = previousByID.get(task.id);
    if (before && JSON.stringify(before) === JSON.stringify(task)) {
      if (prev[index] !== before) changed = true; // same tasks, new order
      return before;
    }
    changed = true;
    return task;
  });
  return changed ? merged : prev;
}

export const DEFAULT_BOARD_COLUMNS: {
  slug: string;
  label: string;
  position: number;
  is_backlog: boolean;
}[] = [
  { slug: "backlog", label: "Backlog", position: 0, is_backlog: true },
  { slug: "todo", label: "Todo", position: 1, is_backlog: false },
  { slug: "in_progress", label: "In Progress", position: 2, is_backlog: false },
  { slug: "analiz_review", label: "Analysis Review", position: 3, is_backlog: false },
  { slug: "code_review", label: "Code Review", position: 4, is_backlog: false },
  { slug: "ready_for_qa", label: "Ready for QA", position: 5, is_backlog: false },
  { slug: "in_qa", label: "In QA", position: 6, is_backlog: false },
  { slug: "need_revision", label: "Need Revision", position: 7, is_backlog: false },
  { slug: "pm_uat", label: "PM UAT", position: 8, is_backlog: false },
  { slug: "human_uat", label: "Human UAT", position: 9, is_backlog: false },
  // Just before Done, matching domain.BoardColumns and migration 056: blocked is
  // off the happy path, so it sits with the terminal columns instead of breaking
  // the left-to-right flow of active work. Omitting it hid every parked card —
  // the board only renders the columns it knows about.
  { slug: "blocked", label: "Blocked", position: 10, is_backlog: false },
  { slug: "done", label: "Done", position: 11, is_backlog: false },
  { slug: "released", label: "Released", position: 12, is_backlog: false },
];

// The types the server ships built in: the four that predate task types as data (task_types
// table, see hooks/useTaskTypes) plus `design` (migration 178). Their locale strings stay the translation
// for these keys as long as nobody has renamed them server-side — see
// taskTypeLabel below.
export const BUILT_IN_TASK_TYPES: { key: string; defaultLabel: string; labelKey: string }[] = [
  { key: "task", defaultLabel: "Task", labelKey: "lib.projectBoard.taskType.task" },
  { key: "analiz", defaultLabel: "Analysis", labelKey: "lib.projectBoard.taskType.analiz" },
  { key: "bug", defaultLabel: "Bug", labelKey: "lib.projectBoard.taskType.bug" },
  { key: "technical", defaultLabel: "Technical", labelKey: "lib.projectBoard.taskType.technical" },
  { key: "design", defaultLabel: "Design", labelKey: "lib.projectBoard.taskType.design" },
];

export const TASK_PRIORITY_OPTIONS: { value: TaskPriority; labelKey: string }[] = [
  { value: "low", labelKey: "lib.projectBoard.taskPriority.low" },
  { value: "medium", labelKey: "lib.projectBoard.taskPriority.medium" },
  { value: "high", labelKey: "lib.projectBoard.taskPriority.high" },
  { value: "critical", labelKey: "lib.projectBoard.taskPriority.critical" },
];

/**
 * A task type's display label. `types` is the loaded task_types list
 * (hooks/useTaskTypes); omitted or not-yet-loaded, this falls back to the
 * built-in locale strings so a board painted before the list arrives still
 * reads correctly for task/analiz/bug/technical/design.
 *
 * For a built-in key whose server label is still the untouched English
 * default ("Task", "Analysis", ...), the locale string is preferred so the
 * label translates with the app language. Once an operator edits that type's
 * label in Settings → Workflows, or for any custom type, the server's label
 * is shown verbatim — it is that operator's own text, not ours to translate.
 */
export function taskTypeLabel(type: TaskType, types?: TaskTypeDef[]): string {
  const builtIn = BUILT_IN_TASK_TYPES.find((b) => b.key === type);
  const server = types?.find((t) => t.key === type);
  if (server) {
    if (builtIn && server.label === builtIn.defaultLabel) {
      const label = tStatic(builtIn.labelKey);
      return label === builtIn.labelKey ? server.label : label;
    }
    return server.label;
  }
  if (builtIn) {
    const label = tStatic(builtIn.labelKey);
    return label === builtIn.labelKey ? type : label;
  }
  return type;
}

/**
 * Select options for a task-type picker. Prefers the loaded task_types list
 * (server order via `position`); falls back to the built-ins when the
 * list has not loaded yet or came back empty (a workspace mid-migration).
 */
export function taskTypeOptions(types: TaskTypeDef[] | undefined): { value: string; label: string }[] {
  if (types && types.length > 0) {
    return [...types]
      .sort((a, b) => a.position - b.position)
      .map((t) => ({ value: t.key, label: taskTypeLabel(t.key, types) }));
  }
  return BUILT_IN_TASK_TYPES.map((b) => ({ value: b.key, label: taskTypeLabel(b.key) }));
}

export function taskPriorityLabel(priority: TaskPriority): string {
  const key = `lib.projectBoard.taskPriority.${priority}`;
  const label = tStatic(key);
  return label === key ? priority : label;
}

/**
 * Label for board_tasks.blocked_resource — the shared thing a parked task is
 * waiting on (domain/resource_block.go). Unknown values fall back to a generic
 * wording rather than leaking the raw key onto a card: the server may learn a
 * new resource before this build ships.
 */
export function blockedResourceLabel(resource: string): string {
  const key = `lib.projectBoard.blockedResource.${resource}`;
  const label = tStatic(key);
  return label === key ? tStatic("lib.projectBoard.blockedResourceFallback") : label;
}

/**
 * Visible label for a work_order park: the blocker task keys themselves
 * (blocked_question reads "waiting for T-12 (API migration) [in_progress] to
 * finish"), not the generic resource wording — a human reading the board
 * needs to know WHICH task it is waiting for without hovering the tooltip.
 *
 * Parsed structurally, not by splitting on ", ": a blocker's TITLE is free
 * text and can itself contain "T-9"-looking tokens or a literal ", " (both
 * happen in this repo's real task titles), so neither a blanket key regex nor
 * a bare comma split is safe. Each blocker segment is "KEY (TITLE) [COLUMN]"
 * (blockerLabels, server work_order.go), and COLUMN is drawn from the fixed
 * set of board column slugs — its closing "]" is the one boundary TITLE can't
 * fake, so segments are found by scanning for "[column_slug]" and the key is
 * read off the front of each segment, up to its first " (".
 */
export function workOrderBlockerLabel(blockedQuestion: string): string {
  const trimmed = blockedQuestion.trim();
  if (!trimmed) {
    return blockedResourceLabel("work_order");
  }
  const prefix = "waiting for ";
  const suffix = " to finish";
  const start = trimmed.startsWith(prefix) ? prefix.length : 0;
  const end = trimmed.endsWith(suffix) ? trimmed.length - suffix.length : trimmed.length;
  const keys = blockerKeys(trimmed.slice(start, end));
  return keys.length > 0 ? blockerKeysLabel(keys) : blockedResourceLabel("work_order");
}

/**
 * Visible label for a deploy_order merge hold. Its detail is the same
 * "KEY (TITLE) [COLUMN]" list a work_order park carries, without the sentence
 * around it (release.Service.mergeGate, server deploy_order.go).
 */
export function deployOrderBlockerLabel(detail: string): string {
  const keys = blockerKeys(detail.trim());
  return keys.length > 0
    ? tStatic("lib.projectBoard.deployOrderBlocker", { keys: blockerKeysLabel(keys) })
    : blockedResourceLabel("deploy_order");
}

function blockerKeysLabel(keys: string[]): string {
  const [first, ...rest] = keys;
  return rest.length > 0 ? `${first} +${rest.length}` : first;
}

function blockerKeys(body: string): string[] {
  const segmentEnd = /\[[a-z0-9_]+\]/gi;
  const keys: string[] = [];
  let cursor = 0;
  let match: RegExpExecArray | null;
  while ((match = segmentEnd.exec(body)) !== null) {
    const segment = body.slice(cursor, match.index + match[0].length);
    const keyMatch = /^\s*,?\s*([^\s(]+)\s*\(/.exec(segment);
    if (keyMatch) keys.push(keyMatch[1]);
    cursor = match.index + match[0].length;
  }
  return keys;
}

/**
 * The merge holds (server domain.MergeHoldResources): why release's merge gate
 * is refusing a done task's merge. Unlike every other park, two of them wait on
 * the human reading the card, not on a sweeper.
 */
export const MERGE_HOLD_RESOURCES = ["deploy_order", "before_deploy", "delivery_profile", "deploy_env"] as const;

export type MergeHoldResource = (typeof MERGE_HOLD_RESOURCES)[number];

export function isMergeHold(resource: string | null | undefined): resource is MergeHoldResource {
  return (MERGE_HOLD_RESOURCES as readonly string[]).includes(resource ?? "");
}

const ORDER_NOTE_OPEN = "<!-- tt:order -->";
const ORDER_NOTE_CLOSE = "<!-- /tt:order -->";

/**
 * before_deploy without the generated order note — server
 * domain.StripOrderNote, literally the same markers and the same reading of an
 * unterminated block. The order note is not a step a human performs (the
 * release gate enforces that order itself), so it never asks for a
 * confirmation.
 */
export function beforeDeploySteps(text: string | null | undefined): string {
  let rest = text ?? "";
  for (;;) {
    const start = rest.indexOf(ORDER_NOTE_OPEN);
    if (start < 0) return rest.trim();
    const after = rest.slice(start + ORDER_NOTE_OPEN.length);
    const end = after.indexOf(ORDER_NOTE_CLOSE);
    rest = end < 0 ? rest.slice(0, start) : rest.slice(0, start) + after.slice(end + ORDER_NOTE_CLOSE.length);
  }
}

/** Server domain.BoardTask.BeforeDeployPending. */
export function isBeforeDeployPending(task: Pick<BoardTask, "before_deploy" | "before_deploy_confirmed_at">): boolean {
  return beforeDeploySteps(task.before_deploy) !== "" && !task.before_deploy_confirmed_at;
}

/**
 * Coarse on purpose: the badge answers "is this stuck?", and a minute-accurate
 * figure only adds noise. The board hands a card this text rather than its
 * clock, so a memoized card re-renders only when what it shows changes.
 */
export function formatColumnAge(iso: string, now: number): string {
  const minutes = Math.max(0, Math.round((now - new Date(iso).getTime()) / 60000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours}h` : `${Math.round(hours / 24)}d`;
}

export const BOARD_TASK_POLL_ACTIVE_MS = 5000;
export const BOARD_TASK_POLL_IDLE_MS = 15000;
export const BOARD_ACTIVITY_POLL_ACTIVE_MS = 2000;
export const BOARD_ACTIVITY_POLL_IDLE_MS = 15000;

/**
 * Fast only while something on the board is moving. A quiet board used to read
 * /v1/activity every 2s for as long as it was open, and since a shared poll
 * runs at its shortest subscriber's interval, it dragged the header's
 * notifications to the same pace.
 */
export function boardPollIntervals(
  tasks: readonly Pick<BoardTask, "column" | "agent_running">[],
  activeAgentTaskIds: ReadonlySet<string>,
): { tasksMs: number; activityMs: number } {
  const agentRunning = activeAgentTaskIds.size > 0 || tasks.some((task) => task.agent_running);
  const moving = agentRunning || tasks.some((task) => task.column === "in_progress");
  return {
    tasksMs: moving ? BOARD_TASK_POLL_ACTIVE_MS : BOARD_TASK_POLL_IDLE_MS,
    activityMs: agentRunning ? BOARD_ACTIVITY_POLL_ACTIVE_MS : BOARD_ACTIVITY_POLL_IDLE_MS,
  };
}

/**
 * Coarse countdown to blocked_resume_at, the mirror image of
 * formatColumnAge and deliberately as coarse: the badge answers "roughly when
 * does this move again?", and a card that re-renders on every poll must not
 * tick seconds down. Already-due parks read "0m" — the sweeper runs on its own
 * schedule, so a passed deadline is still a wait.
 */
export function formatResumeIn(iso: string): string {
  const minutes = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  return hours < 48 ? `${hours}h ${minutes % 60}m` : `${Math.round(hours / 24)}d`;
}

export function columnLabel(
  slug: string,
  columns: { slug: string; label: string }[],
): string {
  return columns.find((c) => c.slug === slug)?.label ?? slug;
}

export function columnLabelMap(
  columns: { slug: string; label: string }[],
): Record<string, string> {
  return Object.fromEntries(columns.map((c) => [c.slug, c.label]));
}

export function boardColumnsSplit(
  columns: { slug: string; label: string; is_backlog: boolean; position: number }[],
) {
  const sorted = [...columns].sort((a, b) => a.position - b.position);
  const backlog = sorted.find((c) => c.is_backlog) ?? sorted[0];
  const board = sorted.filter((c) => !c.is_backlog);
  return { backlog, board };
}

/**
 * Board columns that share one lane, stacked vertically instead of taking a
 * lane each — the board had grown too wide, and the two review stages (like the
 * two UAT stages) are read as one step in practice.
 *
 * This is the single source of truth for the pairing: an ordered list of lanes,
 * each lane an ordered list of column slugs. `boardLanes()` renders from it, so
 * a future pairing is a line here and needs no new markup. Slugs listed here
 * that the board does not have are ignored, and any column *not* listed still
 * gets its own lane — a custom column must never vanish from the board.
 */
export const BOARD_STACKED_LANES: readonly (readonly string[])[] = [
  ["analiz_review", "code_review"],
  ["pm_uat", "human_uat"],
];

export interface BoardLane<C> {
  /** Stable React key: the column slug, or the joined slugs of a stacked lane. */
  key: string;
  /** Stages of this lane, top to bottom. A single entry is a plain column. */
  columns: C[];
}

/**
 * Groups board columns into lanes for rendering.
 *
 * Order is taken from the board's own column order (`position`), never from the
 * grouping table: a lane sits where its first member sits, and stages inside a
 * lane are stacked in workflow order. So `analiz_review` (position 3) renders
 * above `code_review` (4), and `pm_uat` (8) above `human_uat` (9), and reordering
 * the board's columns still gets its own order back.
 */
export function boardLanes<C extends { slug: string; position: number }>(
  columns: C[],
  groups: readonly (readonly string[])[] = BOARD_STACKED_LANES,
): BoardLane<C>[] {
  const groupOf = new Map<string, number>();
  groups.forEach((slugs, index) => {
    for (const slug of slugs) groupOf.set(slug, index);
  });

  const lanes: BoardLane<C>[] = [];
  const laneOfGroup = new Map<number, BoardLane<C>>();

  for (const column of [...columns].sort((a, b) => a.position - b.position)) {
    const group = groupOf.get(column.slug);
    if (group === undefined) {
      lanes.push({ key: column.slug, columns: [column] });
      continue;
    }
    const lane = laneOfGroup.get(group);
    if (lane) {
      lane.columns.push(column);
      continue;
    }
    const created: BoardLane<C> = { key: groups[group].join("+"), columns: [column] };
    laneOfGroup.set(group, created);
    lanes.push(created);
  }

  return lanes;
}

export function indexProgressPercent(filesProcessed: number, filesTotal: number, status: string): number {
  if (filesTotal <= 0) {
    return status === "completed" ? 100 : 0;
  }
  return Math.min(100, Math.round((filesProcessed / filesTotal) * 100));
}

export function pipelineStatusVariant(
  status: TaskPipeline["status"],
): "success" | "info" | "destructive" | "secondary" {
  switch (status) {
    case "success":
      return "success";
    case "failed":
      return "destructive";
    // running/waiting states use "info", not "warning": nothing is wrong yet,
    // and info is the color that reads as distinct from the primary action
    // hue rather than as an in-progress warning.
    case "running":
    case "pending":
      return "info";
    // "skipped" (nothing ran) falls through to the neutral variant on purpose:
    // painting it green implied a build that never happened.
    default:
      return "secondary";
  }
}

// Shared by TaskDetailDrawer's run list and ActivityFeedItem's feed rows so a
// running/pending item never shows "info" in one place and "warning" in the
// other, a few pixels apart on the same board page.
export function runStatusVariant(status: string): "success" | "info" | "destructive" | "secondary" {
  switch (status) {
    case "completed":
      return "success";
    // running/waiting reads as "info", not "warning" — kept distinct from the
    // primary action color without implying something has gone wrong.
    case "running":
    case "pending":
      return "info";
    case "failed":
      return "destructive";
    // Somebody stopped this run on purpose; it is history, not an incident, so
    // it must never borrow the failure colour.
    case "cancelled":
      return "secondary";
    default:
      return "secondary";
  }
}

export function pipelineStatusLabel(status: TaskPipeline["status"]): string {
  const key = `lib.projectBoard.pipelineStatus.${status}`;
  const label = tStatic(key);
  return label === key ? status : label;
}

export function pipelineTriggerLabel(trigger: TaskPipeline["trigger"]): string {
  const key = `lib.projectBoard.pipelineTrigger.${trigger}`;
  const label = tStatic(key);
  return label === key ? trigger : label;
}

// taskPipelineCardIcon maps a board-card-level latest_pipeline_status (a
// plain string from the API, not the narrower TaskPipeline["status"] union)
// to an icon + color for the tiny BoardPage TaskCard badge. Returns null for
// unset/unknown statuses so the card renders nothing.
export function taskPipelineCardIcon(
  status: string | undefined,
  gateReason?: PipelineGateReason,
): { Icon: LucideIcon; className: string } | null {
  // A gate that was opened WITHOUT a build outranks the status, because the
  // status alone lies by omission: the card reads "skipped", which is the same
  // badge an unconfigured repository gets, while what actually happened is that
  // the board gave up waiting for CI (or GitHub said it could never run) and
  // dispatched a reviewer onto an unbuilt diff. That deserves a warning, not a
  // dash — it is the one pipeline outcome a human may want to act on.
  if (gateReason && gateReason !== "gate_disabled") {
    return { Icon: AlertTriangle, className: "text-warning" };
  }
  switch (status) {
    case "success":
      return { Icon: CheckCircle2, className: "text-success" };
    case "failed":
      return { Icon: XCircle, className: "text-destructive" };
    // Static on purpose: a card sits on the board for as long as CI runs, and a
    // spinner on it kept the compositor drawing a frame on every vsync.
    case "pending":
      return { Icon: Clock, className: "text-info" };
    case "running":
      return { Icon: CircleDashed, className: "text-info" };
    // Nothing ran — a muted dash, never the green check the card used to show.
    case "skipped":
      return { Icon: MinusCircle, className: "text-muted-foreground" };
    default:
      return null;
  }
}

// pipelineGateReasonLabel is the sentence a card or panel shows when the review
// gate opened with no green build behind it. "" for an ordinary pipeline.
//
// The spinner on a wedged card was the entire symptom this whole change exists
// to fix, so the replacement must never be another wordless glyph: the user has
// to be able to read the card and know whether CI passed, CI failed, CI could
// not run, or nobody ever asked it to.
export function pipelineGateReasonLabel(reason: PipelineGateReason | undefined): string {
  if (!reason) return "";
  const key = `lib.projectBoard.pipelineGateReason.${reason}`;
  const label = tStatic(key);
  return label === key ? reason : label;
}
