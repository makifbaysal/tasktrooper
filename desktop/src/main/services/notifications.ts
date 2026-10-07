/**
 * Desktop notifications for a board that needs the stakeholder's attention.
 *
 * This lives in the main process, not the renderer: a closed window is hidden
 * and its page throttled (`main/window.ts`), so anything watching from the SPA
 * slows to a crawl exactly when a notification is most needed. `Notification`
 * here is Electron's, from the main process, which a hidden page cannot stall.
 *
 * `diffForNotifications` is pure — no fetch, no Electron — mirroring the
 * `probeHealth`/`waitForHealth` split in `health.ts`. `NotificationWatcher`
 * is the thin I/O shell around it.
 */

import { Notification, powerSaveBlocker } from "electron";
import type { NotificationPreferences } from "../../ipc/types.js";

/**
 * How often the backend is asked. Every tick reads the active runs; the whole
 * task list and the activity feed are read too only when they can say
 * something new (`#tick`).
 *
 * Every 15 s only while a run is in flight, which is when a column change, a
 * comment or a finished chat turn — everything this watcher notifies about —
 * can arrive. A card sitting in "in progress" does not count: it can sit there
 * for days with nothing running. Idle, a minute: nothing on an idle board moves
 * unless the person moves it, and they are looking at it when they do. Both
 * double on battery. `nudge()` is the way back to the fast interval sooner than
 * that: the window losing focus, which is the moment somebody who just started
 * a run walks away from it.
 */
export const POLL_INTERVAL_MS = 15_000;
export const IDLE_POLL_INTERVAL_MS = 60_000;
const BATTERY_FACTOR = 2;
/** A nudge within this long of the last poll is not worth another round of requests. */
const NUDGE_MIN_GAP_MS = 2_000;
/** Consecutive ticks without an `/v1/activity/active` answer before a held suspension blocker is let go. */
export const BLIND_TICKS_BEFORE_RELEASE = 3;
/**
 * A run reported as running for longer than this is taken for one a crashed
 * backend never closed. It no longer holds the suspension blocker or the fast
 * poll: one such row would otherwise hold both for as long as it stays there.
 */
export const STALE_RUN_MS = 6 * 60 * 60_000;

export function pollIntervalMs(state: { busy: boolean; onBattery: boolean }): number {
  const base = state.busy ? POLL_INTERVAL_MS : IDLE_POLL_INTERVAL_MS;
  return state.onBattery ? base * BATTERY_FACTOR : base;
}

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
  /** Where a click should navigate to. `undefined` means the board default. */
  route?: string;
}

export interface DiffResult {
  toNotify: NotificationCandidate[];
  nextSnapshot: TaskSnapshot;
  nextCursor: ActivityCursor | null;
}

export interface RemoteActiveRun {
  id: string;
  // Both present only for an agent-chat run — a board-task run's session has
  // no agent, and the notification below only ever fires for a chat.
  session_id?: string | null;
  agent_id?: string | null;
  title?: string;
  started_at?: string;
}

/**
 * Whether `GET /v1/activity/active` reports any run in flight — board task or
 * agent-chat turn alike, including a chat session that has no board task
 * attached, which is exactly the case that used to let macOS App Nap suspend
 * the process mid-turn. Pass it `liveRuns`, not the raw list.
 */
export function hasActiveRun(runs: RemoteActiveRun[]): boolean {
  return runs.length > 0;
}

/** `runs` without the ones started more than `STALE_RUN_MS` ago. A run with no readable start is kept. */
export function liveRuns(runs: RemoteActiveRun[], now: number): RemoteActiveRun[] {
  return runs.filter((run) => {
    const started = typeof run.started_at === "string" ? Date.parse(run.started_at) : Number.NaN;
    return Number.isNaN(started) || now - started <= STALE_RUN_MS;
  });
}

export interface ActiveChatSession {
  agentId: string;
  title?: string;
}

/** Keyed by session id — the identity a chat-turn notification fires about. */
export type ActiveChatSnapshot = Map<string, ActiveChatSession>;

export interface ChatCompletionDiffResult {
  toNotify: NotificationCandidate[];
  nextSnapshot: ActiveChatSnapshot;
}

/**
 * Diffs one poll's `/v1/activity/active` against the previous poll's set of
 * running chat sessions and returns a notification for every session that
 * dropped out of the running set (the turn finished, one way or another)
 * since the last tick.
 *
 * Unlike `diffForNotifications`, there is no seeding step: a session that was
 * already running at launch and finishes on the very next tick genuinely just
 * finished from this watcher's point of view, and should notify like any
 * other completion.
 */
export function diffChatCompletions(
  prevSnapshot: ActiveChatSnapshot,
  runs: RemoteActiveRun[],
  focusedChat: { agentId: string; sessionId: string } | null,
  isWindowFocused: boolean,
  prefs: NotificationPreferences,
): ChatCompletionDiffResult {
  const nextSnapshot: ActiveChatSnapshot = new Map();
  for (const run of runs) {
    if (!run.session_id || !run.agent_id) continue;
    nextSnapshot.set(run.session_id, { agentId: run.agent_id, title: run.title });
  }

  const toNotify: NotificationCandidate[] = [];
  if (prefs.agentChatReplies) {
    for (const [sessionId, session] of prevSnapshot) {
      if (nextSnapshot.has(sessionId)) continue;

      // The only case a chat-turn notification is suppressed: the window is
      // focused AND that exact session's chat screen is the one on screen.
      const onScreen = isWindowFocused && focusedChat?.agentId === session.agentId && focusedChat.sessionId === sessionId;
      if (onScreen) continue;

      toNotify.push({
        taskId: sessionId,
        title: session.title ? truncate(session.title) : "Agent chat",
        body: "The agent finished replying.",
        route: `/agents/${session.agentId}/chat/${sessionId}`,
      });
    }
  }

  return { toNotify, nextSnapshot };
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

async function readJson<T extends object>(url: string, headers: Record<string, string>): Promise<T | null> {
  try {
    const res = await fetch(url, { headers });
    if (!res.ok) return null;
    const body: unknown = await res.json();
    return typeof body === "object" && body !== null ? (body as T) : null;
  } catch {
    return null;
  }
}

export interface NotificationWatcherOptions {
  apiBase: () => string | null;
  apiToken: () => string | null;
  getPreferences: () => NotificationPreferences;
  onNotificationClick: (route?: string) => void;
  /** Whether the shell's own window currently has OS focus. */
  isWindowFocused: () => boolean;
  /** Whether this machine is on battery; the poll slows down when it is. */
  onBattery?: () => boolean;
  /**
   * Whether the web app is loaded and on screen, polling the board for
   * itself. While it is, the task list and activity feed are read only when
   * the set of active runs changed. Absent means never, which reads them every
   * tick.
   */
  isPageOnScreen?: () => boolean;
}

/**
 * Owns the poll timer, the `fetch` calls, and turning a candidate into a real
 * `Notification`. Everything decidable without I/O lives in
 * `diffForNotifications` above.
 */
export class NotificationWatcher {
  #snapshot: TaskSnapshot | null = null;
  #cursor: ActivityCursor | null = null;
  #timer: ReturnType<typeof setTimeout> | null = null;
  #polling = false;
  #ticking = false;
  #busy = false;
  #lastTickAt = 0;
  #blindTicks = 0;
  // Every run id the last answer listed, stale ones included: a change in it
  // is what makes the board worth reading while the page is on screen.
  #runIds: ReadonlySet<string> = new Set();
  // Successful board reads still owed whatever the page is doing: the seed,
  // and the reads after a change in the run set.
  #boardReadsDue = 1;
  // `prevent-app-suspension` keeps this process's JS running — and so the
  // backend's children supervised — while a run is in flight, which a
  // minimized window would otherwise let macOS App-Nap away. There is
  // deliberately no `prevent-display-sleep`: holding the screen on for as
  // long as any task is in progress kept laptops awake for hours, and nothing
  // a run does needs the display.
  #suspensionBlockerId: number | null = null;
  // Which agent-chat session the page reports as on screen, if any. A
  // chat-turn notification checks it before firing.
  #focusedChat: { agentId: string; sessionId: string } | null = null;
  // The running chat sessions as of the last tick, so the next one can tell
  // which ones dropped out (finished) since then.
  #chatSnapshot: ActiveChatSnapshot = new Map();

  constructor(private readonly options: NotificationWatcherOptions) {}

  setFocusedChat(focus: { agentId: string; sessionId: string } | null): void {
    this.#focusedChat = focus;
  }

  get focusedChat(): { agentId: string; sessionId: string } | null {
    return this.#focusedChat;
  }

  start(): void {
    if (this.#polling) return;
    this.#polling = true;
    this.#blindTicks = 0;
    // Whatever moved while there was no backend to ask.
    this.#boardReadsDue = Math.max(this.#boardReadsDue, 1);
    void this.#poll();
  }

  stop(): void {
    this.#polling = false;
    if (this.#timer) clearTimeout(this.#timer);
    this.#timer = null;
    // The backend that would tell us a run finished just went away (stopped
    // polling, app quitting) — holding the blocker past that point has no
    // signal left that could ever release it.
    this.#releaseSuspensionBlocker();
  }

  /** Poll now rather than at the next interval, unless one just ran or is running. */
  nudge(): void {
    if (!this.#polling || this.#ticking) return;
    if (Date.now() - this.#lastTickAt < NUDGE_MIN_GAP_MS) return;
    if (this.#timer) clearTimeout(this.#timer);
    this.#timer = null;
    void this.#poll();
  }

  async #poll(): Promise<void> {
    this.#timer = null;
    this.#ticking = true;
    try {
      await this.#tick();
    } catch (err) {
      // One bad tick must not end the polling: the next one is the only thing
      // that can ever release the suspension blocker.
      console.warn(`[notifications] poll failed: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      this.#ticking = false;
      this.#lastTickAt = Date.now();
    }
    if (!this.#polling || this.#timer) return;
    const delay = pollIntervalMs({ busy: this.#busy, onBattery: this.options.onBattery?.() ?? false });
    this.#timer = setTimeout(() => void this.#poll(), delay);
  }

  #syncSuspensionGuard(runs: RemoteActiveRun[]): void {
    const shouldBlock = hasActiveRun(runs);
    if (shouldBlock && this.#suspensionBlockerId === null) {
      // Electron reports a start failure as -1 rather than throwing; treating
      // it as "unsupported here" instead of storing it keeps a later, real id
      // (0 is valid) from being mistaken for "not blocked".
      const id = powerSaveBlocker.start("prevent-app-suspension");
      if (id >= 0) this.#suspensionBlockerId = id;
    } else if (!shouldBlock) {
      this.#releaseSuspensionBlocker();
    }
  }

  #releaseSuspensionBlocker(): void {
    if (this.#suspensionBlockerId === null) return;
    powerSaveBlocker.stop(this.#suspensionBlockerId);
    this.#suspensionBlockerId = null;
  }

  async #tick(): Promise<void> {
    const base = this.options.apiBase();
    if (!base) return;

    const headers = { Authorization: `Bearer ${this.options.apiToken() ?? ""}` };
    const activeRunsBody = await readJson<{ runs?: RemoteActiveRun[] | null }>(`${base}/v1/activity/active`, headers);

    if (!activeRunsBody) {
      this.#blindTicks++;
      // A held blocker is only ever released by a tick that saw no run; with
      // no answer for this long it is released anyway, and the next answer
      // that shows a run takes it again.
      if (this.#blindTicks >= BLIND_TICKS_BEFORE_RELEASE) this.#releaseSuspensionBlocker();
      return;
    }
    this.#blindTicks = 0;
    // The endpoints serialize an empty Go slice as `null`, not `[]`.
    const activeRuns = activeRunsBody.runs ?? [];
    const live = liveRuns(activeRuns, Date.now());
    this.#syncSuspensionGuard(live);
    this.#busy = hasActiveRun(live);

    const runIds = new Set(activeRuns.map((run) => run.id));
    const started = activeRuns.some((run) => !this.#runIds.has(run.id));
    // Twice: a run can leave the active list a moment before the runner
    // moves its card, and the read on the very tick it left would miss that.
    if (started || runIds.size !== this.#runIds.size) this.#boardReadsDue = 2;
    this.#runIds = runIds;

    let board: { tasks: RemoteTask[]; items: RemoteActivityItem[] } | null = null;
    if (this.#boardReadsDue > 0 || !(this.options.isPageOnScreen?.() ?? false)) {
      const [tasksBody, activityBody] = await Promise.all([
        readJson<{ tasks?: RemoteTask[] | null }>(`${base}/v1/tasks`, headers),
        readJson<{ items?: RemoteActivityItem[] | null }>(`${base}/v1/activity?limit=50`, headers),
      ]);
      // A failed read (a network blip, a backend mid-restart) is "try again next
      // tick", not a reason to treat the next real poll as a seed.
      if (tasksBody && activityBody) board = { tasks: tasksBody.tasks ?? [], items: activityBody.items ?? [] };
    }

    const prefs = this.options.getPreferences();
    const toNotify: NotificationCandidate[] = [];
    if (board) {
      const diff = diffForNotifications(this.#snapshot, board.tasks, board.items, this.#cursor, prefs);
      this.#snapshot = diff.nextSnapshot;
      this.#cursor = diff.nextCursor;
      this.#boardReadsDue = Math.max(0, this.#boardReadsDue - 1);
      toNotify.push(...diff.toNotify);
    }

    const chatDiff = diffChatCompletions(
      this.#chatSnapshot,
      activeRuns,
      this.#focusedChat,
      this.options.isWindowFocused(),
      prefs,
    );
    this.#chatSnapshot = chatDiff.nextSnapshot;
    toNotify.push(...chatDiff.toNotify);

    if (!prefs.enabled || !Notification.isSupported()) return;
    for (const candidate of toNotify) {
      const notification = new Notification({ title: candidate.title, body: candidate.body });
      notification.on("click", () => this.options.onNotificationClick(candidate.route));
      notification.show();
    }
  }
}
