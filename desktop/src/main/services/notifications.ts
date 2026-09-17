/**
 * Desktop notifications for a board that needs the stakeholder's attention.
 *
 * This lives in the main process, not the renderer: the window is destroyed
 * (not hidden) on close (`main/window.ts`), so anything watching from the SPA
 * stops running exactly when a notification is most needed. `Notification`
 * here is Electron's, from the main process, which survives a closed window.
 *
 * `diffForNotifications` is pure — no fetch, no Electron — mirroring the
 * `probeHealth`/`waitForHealth` split in `health.ts`. `NotificationWatcher`
 * is the thin I/O shell around it.
 */

import { Notification } from "electron";
import type { NotificationPreferences } from "../../ipc/types.js";

export const POLL_INTERVAL_MS = 15_000;

const BODY_MAX = 120;

export interface RemoteTask {
  id: string;
  key: string;
  title: string;
  column: string;
  blocked_question?: string;
  blocked_resource?: string;
}

export interface RemoteActivityItem {
  id: string;
  kind: string;
  task_id?: string;
  event_type?: string;
  payload?: Record<string, unknown>;
  created_at: string;
}

export interface TaskSnapshotEntry {
  column: string;
  blockedQuestion?: string;
  blockedResource?: string;
}

export type TaskSnapshot = Map<string, TaskSnapshotEntry>;

export interface ActivityCursor {
  lastEventId: string;
  lastEventAt: string;
}

export interface NotificationCandidate {
  taskId: string;
  title: string;
  body: string;
}

export interface DiffResult {
  toNotify: NotificationCandidate[];
  nextSnapshot: TaskSnapshot;
  nextCursor: ActivityCursor | null;
}

function truncate(s: string, max = BODY_MAX): string {
  if (s.length <= max) return s;
  return `${s.slice(0, max - 1).trimEnd()}…`;
}

/** The chronologically later of two cursors, `null` losing to anything. */
function laterCursor(a: ActivityCursor | null, b: ActivityCursor | null): ActivityCursor | null {
  if (!a) return b;
  if (!b) return a;
  if (a.lastEventAt !== b.lastEventAt) return a.lastEventAt > b.lastEventAt ? a : b;
  return a.lastEventId >= b.lastEventId ? a : b;
}

function isAfterCursor(item: RemoteActivityItem, cursor: ActivityCursor): boolean {
  if (item.created_at !== cursor.lastEventAt) return item.created_at > cursor.lastEventAt;
  return item.id !== cursor.lastEventId;
}

function newestOf(items: RemoteActivityItem[]): ActivityCursor | null {
  return items.reduce<ActivityCursor | null>(
    (best, item) => laterCursor(best, { lastEventId: item.id, lastEventAt: item.created_at }),
    null,
  );
}

/**
 * Diffs one poll's `/v1/tasks` + `/v1/activity` against the previous poll's
 * state and returns the notifications that should fire.
 *
 * The very first call for each of `prevSnapshot`/`prevCursor` is a seed: it
 * records the current state but notifies nothing, so relaunching the app does
 * not replay every task already sitting in a gate column or every comment
 * still inside the activity window.
 */
export function diffForNotifications(
  prevSnapshot: TaskSnapshot | null,
  tasks: RemoteTask[],
  activityItems: RemoteActivityItem[],
  prevCursor: ActivityCursor | null,
  prefs: NotificationPreferences,
): DiffResult {
  const nextSnapshot: TaskSnapshot = new Map();
  const toNotify: NotificationCandidate[] = [];
  const seedingTasks = prevSnapshot === null;

  for (const task of tasks) {
    const entry: TaskSnapshotEntry = {
      column: task.column,
      blockedQuestion: task.blocked_question,
      blockedResource: task.blocked_resource,
    };
    nextSnapshot.set(task.id, entry);

    if (seedingTasks) continue;
    const prev = prevSnapshot.get(task.id);
    if (!prev || prev.column === entry.column) continue;

    if (entry.column === "analiz_review" && prefs.analizReview) {
      toNotify.push({ taskId: task.id, title: `${task.key} — Ready for your review`, body: truncate(task.title) });
    } else if (entry.column === "human_uat" && prefs.humanUat) {
      toNotify.push({ taskId: task.id, title: `${task.key} — Awaiting your UAT`, body: truncate(task.title) });
    } else if (entry.column === "blocked" && prefs.humanNeeded) {
      // A human-decision park carries its detail line in blockedQuestion too
      // (see BoardTask.blocked_resource), so this check has to come first —
      // otherwise every T4 would also fire as a T3.
      if (entry.blockedResource === "human_decision") {
        toNotify.push({ taskId: task.id, title: `${task.key} — Needs your decision`, body: truncate(task.title) });
      } else if (entry.blockedQuestion) {
        toNotify.push({ taskId: task.id, title: `${task.key} — An agent has a question`, body: truncate(task.title) });
      }
    }
  }

  const commentItems = activityItems.filter((item) => item.kind === "board_event");
  const nextCursor = laterCursor(prevCursor, newestOf(commentItems));

  if (prevCursor !== null) {
    for (const item of commentItems) {
      if (!isAfterCursor(item, prevCursor)) continue;
      if (item.event_type !== "task.commented") continue;
      if (item.payload?.author_type !== "agent") continue;
      if (!prefs.agentComments) continue;

      const task = tasks.find((t) => t.id === item.task_id);
      const label = task?.key ?? "A task";
      const authorName = typeof item.payload.author_name === "string" ? item.payload.author_name : "An agent";
      const content = typeof item.payload.content === "string" ? item.payload.content : "";
      toNotify.push({
        taskId: item.task_id ?? "",
        title: `${label} — New comment`,
        body: truncate(`${authorName}: ${content}`),
      });
    }
  }

  return { toNotify, nextSnapshot, nextCursor };
}

export interface NotificationWatcherOptions {
  apiBase: () => string | null;
  apiToken: () => string | null;
  getPreferences: () => NotificationPreferences;
  onNotificationClick: () => void;
}

/**
 * Owns the poll timer, the two `fetch` calls, and turning a candidate into a
 * real `Notification`. Everything decidable without I/O lives in
 * `diffForNotifications` above, which is why this class has no test of its
 * own beyond typechecking — there is nothing left here to assert against
 * without mocking Electron and the network both.
 */
export class NotificationWatcher {
  #snapshot: TaskSnapshot | null = null;
  #cursor: ActivityCursor | null = null;
  #timer: ReturnType<typeof setInterval> | null = null;

  constructor(private readonly options: NotificationWatcherOptions) {}

  start(): void {
    if (this.#timer) return;
    this.#timer = setInterval(() => void this.#tick(), POLL_INTERVAL_MS);
    void this.#tick();
  }

  stop(): void {
    if (this.#timer) clearInterval(this.#timer);
    this.#timer = null;
  }

  async #tick(): Promise<void> {
    const base = this.options.apiBase();
    if (!base) return;

    let tasks: RemoteTask[];
    let activityItems: RemoteActivityItem[];
    try {
      const headers = { Authorization: `Bearer ${this.options.apiToken() ?? ""}` };
      const [tasksRes, activityRes] = await Promise.all([
        fetch(`${base}/v1/tasks`, { headers }),
        fetch(`${base}/v1/activity?limit=50`, { headers }),
      ]);
      if (!tasksRes.ok || !activityRes.ok) return;
      const tasksBody = (await tasksRes.json()) as { tasks: RemoteTask[] };
      const activityBody = (await activityRes.json()) as { items: RemoteActivityItem[] };
      tasks = tasksBody.tasks;
      activityItems = activityBody.items;
    } catch {
      // A network blip or a backend mid-restart is "try again next tick", not
      // a reason to treat the next real poll as a seed.
      return;
    }

    const prefs = this.options.getPreferences();
    const { toNotify, nextSnapshot, nextCursor } = diffForNotifications(
      this.#snapshot,
      tasks,
      activityItems,
      this.#cursor,
      prefs,
    );
    this.#snapshot = nextSnapshot;
    this.#cursor = nextCursor;

    if (!prefs.enabled || !Notification.isSupported()) return;
    for (const candidate of toNotify) {
      const notification = new Notification({ title: candidate.title, body: candidate.body });
      notification.on("click", this.options.onNotificationClick);
      notification.show();
    }
  }
}
