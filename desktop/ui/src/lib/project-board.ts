import { AlertTriangle, CheckCircle2, Loader2, MinusCircle, XCircle, type LucideIcon } from "lucide-react";
import type {
  BoardTask,
  PipelineGateReason,
  TaskPipeline,
  TaskPriority,
  TaskType,
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

// labelKey resolves against the i18n `lib.projectBoard.*` namespace; render with
// t(o.labelKey) in components (reactive to the language switch).
export const TASK_TYPE_OPTIONS: { value: TaskType; labelKey: string }[] = [
  { value: "task", labelKey: "lib.projectBoard.taskType.task" },
  { value: "analiz", labelKey: "lib.projectBoard.taskType.analiz" },
  { value: "bug", labelKey: "lib.projectBoard.taskType.bug" },
];

export const TASK_PRIORITY_OPTIONS: { value: TaskPriority; labelKey: string }[] = [
  { value: "low", labelKey: "lib.projectBoard.taskPriority.low" },
  { value: "medium", labelKey: "lib.projectBoard.taskPriority.medium" },
  { value: "high", labelKey: "lib.projectBoard.taskPriority.high" },
  { value: "critical", labelKey: "lib.projectBoard.taskPriority.critical" },
];

export function taskTypeLabel(type: TaskType): string {
  const key = `lib.projectBoard.taskType.${type}`;
  const label = tStatic(key);
  return label === key ? type : label;
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
  const body = trimmed.slice(start, end);

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

  if (keys.length === 0) {
    return blockedResourceLabel("work_order");
  }
  const [first, ...rest] = keys;
  return rest.length > 0 ? `${first} +${rest.length}` : first;
}

/**
 * Coarse countdown to blocked_resume_at, the mirror image of BoardPage's
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
): "success" | "warning" | "destructive" | "secondary" {
  switch (status) {
    case "success":
      return "success";
    case "failed":
      return "destructive";
    case "running":
    case "pending":
      return "warning";
    // "skipped" (nothing ran) falls through to the neutral variant on purpose:
    // painting it green implied a build that never happened.
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
    case "pending":
    case "running":
      return { Icon: Loader2, className: "text-warning animate-spin" };
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
