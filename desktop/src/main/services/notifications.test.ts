import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NotificationPreferences } from "../../ipc/types.js";
import {
  type ActiveChatSnapshot,
  BLIND_TICKS_BEFORE_RELEASE,
  diffChatCompletions,
  diffForNotifications,
  hasActiveRun,
  IDLE_POLL_INTERVAL_MS,
  liveRuns,
  NotificationWatcher,
  POLL_INTERVAL_MS,
  pollIntervalMs,
  type RemoteActiveRun,
  type RemoteActivityItem,
  type RemoteTask,
  STALE_RUN_MS,
} from "./notifications.js";

const powerSaveBlockerStart = vi.fn<(type: string) => number>();
const powerSaveBlockerStop = vi.fn<(id: number) => void>();

const shown = vi.hoisted(() => ({ supported: false, titles: [] as string[] }));

vi.mock("electron", () => ({
  Notification: class {
    static isSupported(): boolean {
      return shown.supported;
    }
    constructor(readonly opts: { title: string }) {}
    on(): void {}
    show(): void {
      shown.titles.push(this.opts.title);
    }
  },
  powerSaveBlocker: {
    start: (type: string) => powerSaveBlockerStart(type),
    stop: (id: number) => powerSaveBlockerStop(id),
  },
}));

function allOn(): NotificationPreferences {
  return {
    enabled: true,
    analizReview: true,
    humanUat: true,
    humanNeeded: true,
    agentComments: true,
    agentChatReplies: true,
  };
}

function task(overrides: Partial<RemoteTask> & Pick<RemoteTask, "id" | "key">): RemoteTask {
  return { title: "A task title", column: "todo", ...overrides };
}

describe("diffForNotifications", () => {
  it("seeds without notifying, but populates the snapshot and cursor", () => {
    const tasks = [task({ id: "t1", key: "T-1", column: "analiz_review" })];
    const activity: RemoteActivityItem[] = [
      { id: "e1", kind: "board_event", event_type: "task.commented", task_id: "t1", created_at: "2026-01-01T00:00:00Z", payload: { author_type: "agent", author_name: "dev", content: "hi" } },
    ];
    const result = diffForNotifications(null, tasks, activity, null, allOn());
    expect(result.toNotify).toEqual([]);
    expect(result.nextSnapshot.get("t1")).toEqual({ column: "analiz_review", blockedQuestion: undefined, blockedResource: undefined });
    expect(result.nextCursor).toEqual({ lastEventId: "e1", lastEventAt: "2026-01-01T00:00:00Z" });
  });

  it("notifies once when a task enters analiz_review", () => {
    const prevTasks = [task({ id: "t1", key: "T-1", column: "todo" })];
    const seed = diffForNotifications(null, prevTasks, [], null, allOn());

    const nextTasks = [task({ id: "t1", key: "T-1", column: "analiz_review", title: "Ship the thing" })];
    const result = diffForNotifications(seed.nextSnapshot, nextTasks, [], seed.nextCursor, allOn());

    expect(result.toNotify).toEqual([{ taskId: "t1", title: "T-1 — Ready for your review", body: "Ship the thing" }]);
  });

  it("does not notify again when the column has not changed", () => {
    const tasks = [task({ id: "t1", key: "T-1", column: "analiz_review" })];
    const seed = diffForNotifications(null, tasks, [], null, allOn());
    const result = diffForNotifications(seed.nextSnapshot, tasks, [], seed.nextCursor, allOn());
    expect(result.toNotify).toEqual([]);
  });

  it("notifies T3 for a human question and T4 for a human-decision park, never both", () => {
    const before = [task({ id: "t1", key: "T-1", column: "in_progress" })];
    const seed = diffForNotifications(null, before, [], null, allOn());

    const questionBlock = [
      task({ id: "t1", key: "T-1", column: "blocked", blocked_question: "Which provider?" }),
    ];
    const questionResult = diffForNotifications(seed.nextSnapshot, questionBlock, [], seed.nextCursor, allOn());
    expect(questionResult.toNotify).toEqual([
      { taskId: "t1", title: "T-1 — An agent has a question", body: "A task title" },
    ]);

    const decisionBlock = [
      task({ id: "t1", key: "T-1", column: "blocked", blocked_question: "Needs a call", blocked_resource: "human_decision" }),
    ];
    const decisionResult = diffForNotifications(seed.nextSnapshot, decisionBlock, [], seed.nextCursor, allOn());
    expect(decisionResult.toNotify).toEqual([
      { taskId: "t1", title: "T-1 — Needs your decision", body: "A task title" },
    ]);
  });

  it("notifies for an agent comment but not a human comment", () => {
    const tasks = [task({ id: "t1", key: "T-1" })];
    const seed = diffForNotifications(null, tasks, [], null, allOn());

    const humanComment: RemoteActivityItem[] = [
      { id: "e1", kind: "board_event", event_type: "task.commented", task_id: "t1", created_at: "2026-01-02T00:00:00Z", payload: { author_type: "human", content: "thanks" } },
    ];
    const humanResult = diffForNotifications(seed.nextSnapshot, tasks, humanComment, seed.nextCursor, allOn());
    expect(humanResult.toNotify).toEqual([]);

    const agentComment: RemoteActivityItem[] = [
      { id: "e2", kind: "board_event", event_type: "task.commented", task_id: "t1", created_at: "2026-01-02T00:01:00Z", payload: { author_type: "agent", author_name: "dev-agent", content: "Needs your input on the API shape" } },
    ];
    const agentResult = diffForNotifications(humanResult.nextSnapshot, tasks, agentComment, humanResult.nextCursor, allOn());
    expect(agentResult.toNotify).toEqual([
      { taskId: "t1", title: "T-1 — New comment", body: "dev-agent: Needs your input on the API shape" },
    ]);
  });

  it("does not repeat a notification for the same activity item on the next poll", () => {
    const tasks = [task({ id: "t1", key: "T-1" })];
    const olderItem: RemoteActivityItem = {
      id: "e0",
      kind: "board_event",
      event_type: "task.commented",
      task_id: "t1",
      created_at: "2026-01-01T00:00:00Z",
      payload: { author_type: "human", content: "seed baseline" },
    };
    const seed = diffForNotifications(null, tasks, [olderItem], null, allOn());

    const activity: RemoteActivityItem[] = [
      { id: "e1", kind: "board_event", event_type: "task.commented", task_id: "t1", created_at: "2026-01-02T00:00:00Z", payload: { author_type: "agent", author_name: "dev", content: "hello" } },
    ];
    const first = diffForNotifications(seed.nextSnapshot, tasks, activity, seed.nextCursor, allOn());
    expect(first.toNotify).toHaveLength(1);

    const second = diffForNotifications(first.nextSnapshot, tasks, activity, first.nextCursor, allOn());
    expect(second.toNotify).toEqual([]);
  });

  it("respects a disabled category while other categories keep working", () => {
    const before = [task({ id: "t1", key: "T-1", column: "todo" }), task({ id: "t2", key: "T-2", column: "todo" })];
    const seed = diffForNotifications(null, before, [], null, allOn());

    const prefs: NotificationPreferences = { ...allOn(), humanUat: false };
    const after = [
      task({ id: "t1", key: "T-1", column: "human_uat" }),
      task({ id: "t2", key: "T-2", column: "analiz_review" }),
    ];
    const result = diffForNotifications(seed.nextSnapshot, after, [], seed.nextCursor, prefs);
    expect(result.toNotify).toEqual([{ taskId: "t2", title: "T-2 — Ready for your review", body: "A task title" }]);
  });
});

describe("hasActiveRun", () => {
  it("is false for an empty run list and true once one exists", () => {
    expect(hasActiveRun([])).toBe(false);
    expect(hasActiveRun([{ id: "r1" }])).toBe(true);
  });
});

describe("liveRuns", () => {
  const now = Date.parse("2026-10-07T18:00:00Z");
  const startedAgo = (ms: number): RemoteActiveRun => ({ id: `r${ms}`, started_at: new Date(now - ms).toISOString() });

  it("drops a run started more than six hours ago and keeps one inside the cutoff", () => {
    const fresh = startedAgo(5 * 60 * 60_000);
    const edge = startedAgo(STALE_RUN_MS);
    const stale = startedAgo(STALE_RUN_MS + 1);
    expect(liveRuns([fresh, edge, stale], now)).toEqual([fresh, edge]);
  });

  it("keeps a run whose start it cannot read", () => {
    const runs: RemoteActiveRun[] = [{ id: "a" }, { id: "b", started_at: "not a date" }];
    expect(liveRuns(runs, now)).toEqual(runs);
  });

  it("reads the backend's nanosecond timestamps", () => {
    const run: RemoteActiveRun = { id: "r", started_at: "2026-10-07T11:59:59.123456789Z" };
    expect(liveRuns([run], now)).toEqual([]);
  });
});

describe("diffChatCompletions", () => {
  const chatRun = (overrides: Partial<RemoteActiveRun> = {}): RemoteActiveRun => ({
    id: "r1",
    session_id: "s1",
    agent_id: "a1",
    title: "Refactor the auth flow",
    ...overrides,
  });

  function running(run: RemoteActiveRun): ActiveChatSnapshot {
    return diffChatCompletions(new Map(), [run], null, false, allOn()).nextSnapshot;
  }

  it("does not notify while the session is still running", () => {
    const result = diffChatCompletions(new Map(), [chatRun()], null, false, allOn());
    expect(result.toNotify).toEqual([]);
    expect(result.nextSnapshot.get("s1")).toEqual({ agentId: "a1", title: "Refactor the auth flow" });
  });

  it("suppresses the notification when the window is focused on that exact chat", () => {
    const prev = running(chatRun());
    const result = diffChatCompletions(prev, [], { agentId: "a1", sessionId: "s1" }, true, allOn());
    expect(result.toNotify).toEqual([]);
  });

  it("notifies when the window is unfocused/backgrounded, even if that chat was the one open", () => {
    const prev = running(chatRun());
    const result = diffChatCompletions(prev, [], { agentId: "a1", sessionId: "s1" }, false, allOn());
    expect(result.toNotify).toEqual([
      {
        taskId: "s1",
        title: "Refactor the auth flow",
        body: "The agent finished replying.",
        route: "/agents/a1/chat/s1",
      },
    ]);
  });

  it("notifies when the window is focused but on a different screen or session", () => {
    const prev = running(chatRun());

    const differentSession = diffChatCompletions(prev, [], { agentId: "a1", sessionId: "s2" }, true, allOn());
    expect(differentSession.toNotify).toHaveLength(1);

    const noChatOpen = diffChatCompletions(prev, [], null, true, allOn());
    expect(noChatOpen.toNotify).toHaveLength(1);
  });

  it("falls back to a generic title when the session has none yet", () => {
    const prev = running(chatRun({ title: undefined }));
    const result = diffChatCompletions(prev, [], null, false, allOn());
    expect(result.toNotify).toEqual([
      { taskId: "s1", title: "Agent chat", body: "The agent finished replying.", route: "/agents/a1/chat/s1" },
    ]);
  });

  it("respects the agentChatReplies preference", () => {
    const prev = running(chatRun());
    const prefs: NotificationPreferences = { ...allOn(), agentChatReplies: false };
    const result = diffChatCompletions(prev, [], null, false, prefs);
    expect(result.toNotify).toEqual([]);
  });

  it("does not notify for a board-task run without a session/agent id", () => {
    const boardRun: RemoteActiveRun = { id: "r2" };
    const seeded = diffChatCompletions(new Map(), [boardRun], null, false, allOn());
    expect(seeded.nextSnapshot.size).toBe(0);

    const result = diffChatCompletions(seeded.nextSnapshot, [], null, false, allOn());
    expect(result.toNotify).toEqual([]);
  });
});

/**
 * The suspension guard, exercised through `NotificationWatcher#tick` rather
 * than in isolation: what matters is that `/v1/activity/active` actually
 * drives `powerSaveBlocker`, not just that `hasActiveRun` computes the right
 * boolean in a vacuum.
 */
describe("NotificationWatcher — prevent-app-suspension", () => {
  let activeRuns: RemoteActiveRun[] = [];
  const prefs = (): NotificationPreferences => allOn();

  function fetchMock(url: string): Promise<{ ok: boolean; json: () => Promise<unknown> }> {
    if (url.endsWith("/v1/tasks")) return Promise.resolve({ ok: true, json: () => Promise.resolve({ tasks: [] }) });
    if (url.endsWith("/v1/activity?limit=50")) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ items: [] }) });
    }
    if (url.endsWith("/v1/activity/active")) {
      // The real backend serializes an empty Go slice as `null`, not `[]` —
      // reproduce that here rather than the friendlier `[]` a naive mock
      // would return, since that is exactly what let the null-handling bug
      // through review once already.
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ runs: activeRuns.length > 0 ? activeRuns : null }) });
    }
    throw new Error(`unexpected fetch: ${url}`);
  }

  beforeEach(() => {
    activeRuns = [];
    vi.useFakeTimers();
    vi.stubGlobal("fetch", vi.fn(fetchMock));
    powerSaveBlockerStart.mockReset();
    powerSaveBlockerStart.mockReturnValue(7);
    powerSaveBlockerStop.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("starts prevent-app-suspension once /v1/activity/active reports a run", async () => {
    activeRuns = [{ id: "r1" }];
    const watcher = new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1234",
      apiToken: () => "token",
      getPreferences: prefs,
      onNotificationClick: () => {},
      isWindowFocused: () => false,
    });

    watcher.start();
    await vi.advanceTimersByTimeAsync(0);

    expect(powerSaveBlockerStart).toHaveBeenCalledWith("prevent-app-suspension");
    watcher.stop();
  });

  it("releases prevent-app-suspension once the run list goes empty", async () => {
    activeRuns = [{ id: "r1" }];
    const watcher = new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1234",
      apiToken: () => "token",
      getPreferences: prefs,
      onNotificationClick: () => {},
      isWindowFocused: () => false,
    });

    watcher.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(powerSaveBlockerStart).toHaveBeenCalledWith("prevent-app-suspension");

    activeRuns = [];
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);

    expect(powerSaveBlockerStop).toHaveBeenCalledWith(7);
    watcher.stop();
  });

  // Regression: `/v1/activity/active` answering `{"runs":null}` (the real
  // shape once no run is in flight) used to throw inside `#tick` on
  // `null.length`, which — because the throw happened before the tasks/
  // activity fetch results were ever diffed — also silently stopped board
  // notifications (analiz review, human UAT, comments) from firing.
  it("tolerates runs:null without throwing and keeps polling board tasks", async () => {
    activeRuns = [];
    let tasksPolled = 0;
    const originalFetch = fetchMock;
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        if (url.endsWith("/v1/tasks")) tasksPolled += 1;
        return originalFetch(url);
      }),
    );

    const watcher = new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1234",
      apiToken: () => "token",
      getPreferences: prefs,
      onNotificationClick: () => {},
      isWindowFocused: () => false,
    });

    watcher.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);

    expect(tasksPolled).toBeGreaterThanOrEqual(3);
    expect(powerSaveBlockerStart).not.toHaveBeenCalledWith("prevent-app-suspension");
    watcher.stop();
  });

  it("keeps polling after a tick throws, and still releases the blocker once the run ends", async () => {
    activeRuns = [{ id: "r1" }];
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    let broken = true;
    const watcher = new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1234",
      apiToken: () => "token",
      getPreferences: () => {
        if (broken) throw new Error("settings unreadable");
        return allOn();
      },
      onNotificationClick: () => {},
      isWindowFocused: () => false,
    });

    watcher.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(powerSaveBlockerStart).toHaveBeenCalledWith("prevent-app-suspension");
    expect(warn).toHaveBeenCalled();

    activeRuns = [];
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(powerSaveBlockerStop).toHaveBeenCalledWith(7);

    broken = false;
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(vi.mocked(fetch).mock.calls.filter(([url]) => String(url).endsWith("/v1/tasks"))).toHaveLength(3);
    watcher.stop();
    warn.mockRestore();
  });

  it("tolerates tasks:null and items:null while a run holds the blocker", async () => {
    activeRuns = [{ id: "r1" }];
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        if (url.endsWith("/v1/tasks")) return Promise.resolve({ ok: true, json: () => Promise.resolve({ tasks: null }) });
        if (url.endsWith("/v1/activity?limit=50")) return Promise.resolve({ ok: true, json: () => Promise.resolve({ items: null }) });
        return fetchMock(url);
      }),
    );
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const watcher = new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1234",
      apiToken: () => "token",
      getPreferences: prefs,
      onNotificationClick: () => {},
      isWindowFocused: () => false,
    });

    watcher.start();
    await vi.advanceTimersByTimeAsync(0);
    activeRuns = [];
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);

    expect(powerSaveBlockerStop).toHaveBeenCalledWith(7);
    expect(warn).not.toHaveBeenCalled();
    watcher.stop();
    warn.mockRestore();
  });

  /**
   * A crashed backend can leave a chat run marked running for good. The
   * blocker must not be held for it for as long as that row exists.
   */
  it("never takes the blocker for a run older than six hours, and lets go of a held run that crosses the cutoff", async () => {
    activeRuns = [{ id: "old", started_at: new Date(Date.now() - STALE_RUN_MS - 1_000).toISOString() }];
    const watcher = new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1234",
      apiToken: () => "token",
      getPreferences: prefs,
      onNotificationClick: () => {},
      isWindowFocused: () => false,
    });
    watcher.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(powerSaveBlockerStart).not.toHaveBeenCalled();
    watcher.stop();

    activeRuns = [{ id: "long", started_at: new Date(Date.now() - STALE_RUN_MS + 20_000).toISOString() }];
    watcher.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(powerSaveBlockerStart).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(powerSaveBlockerStop).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(powerSaveBlockerStop).toHaveBeenCalledWith(7);
    watcher.stop();
  });

  it("lets go of the blocker when the active-run list cannot be read for several ticks", async () => {
    activeRuns = [{ id: "r1" }];
    let activeDown = false;
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        if (activeDown && url.endsWith("/v1/activity/active")) return Promise.resolve({ ok: false, json: () => Promise.resolve({}) });
        return fetchMock(url);
      }),
    );
    const watcher = new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1234",
      apiToken: () => "token",
      getPreferences: prefs,
      onNotificationClick: () => {},
      isWindowFocused: () => false,
    });

    watcher.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(powerSaveBlockerStart).toHaveBeenCalledTimes(1);

    activeDown = true;
    for (let i = 1; i < BLIND_TICKS_BEFORE_RELEASE; i++) {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
      expect(powerSaveBlockerStop).not.toHaveBeenCalled();
    }
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(powerSaveBlockerStop).toHaveBeenCalledWith(7);

    activeDown = false;
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(powerSaveBlockerStart).toHaveBeenCalledTimes(2);
    watcher.stop();
  });
});

describe("pollIntervalMs", () => {
  it("polls fast only while something is running, and slower on battery", () => {
    expect(pollIntervalMs({ busy: true, onBattery: false })).toBe(15_000);
    expect(pollIntervalMs({ busy: false, onBattery: false })).toBe(60_000);
    expect(pollIntervalMs({ busy: true, onBattery: true })).toBe(30_000);
    expect(pollIntervalMs({ busy: false, onBattery: true })).toBe(120_000);
  });
});

describe("NotificationWatcher — how often it asks", () => {
  let tasks: RemoteTask[] = [];
  let runs: RemoteActiveRun[] = [];
  let polls = 0;

  beforeEach(() => {
    tasks = [];
    runs = [];
    polls = 0;
    vi.useFakeTimers();
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        if (url.endsWith("/v1/tasks")) {
          polls += 1;
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ tasks }) });
        }
        if (url.endsWith("/v1/activity?limit=50")) return Promise.resolve({ ok: true, json: () => Promise.resolve({ items: [] }) });
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ runs: runs.length > 0 ? runs : null }) });
      }),
    );
    powerSaveBlockerStart.mockReset();
    powerSaveBlockerStart.mockReturnValue(3);
    powerSaveBlockerStop.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  function watcher(onBattery = false): NotificationWatcher {
    return new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1",
      apiToken: () => "t",
      getPreferences: () => allOn(),
      onNotificationClick: () => {},
      isWindowFocused: () => true,
      onBattery: () => onBattery,
    });
  }

  it("asks once a minute on an idle board", async () => {
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(polls).toBe(1);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(polls).toBe(1);
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS - POLL_INTERVAL_MS);
    expect(polls).toBe(2);
    w.stop();
  });

  it("asks every 15 s while a run is in flight, and never holds the display awake for it", async () => {
    runs = [{ id: "r1", started_at: new Date().toISOString() }];
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(polls).toBe(2);
    expect(powerSaveBlockerStart).not.toHaveBeenCalledWith("prevent-display-sleep");
    w.stop();
  });

  /** A card can sit in "in progress" for days with nothing running behind it. */
  it("asks once a minute while a card sits in progress with no run behind it", async () => {
    tasks = [task({ id: "t1", key: "T-1", column: "in_progress" })];
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(polls).toBe(1);
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS - POLL_INTERVAL_MS);
    expect(polls).toBe(2);
    expect(powerSaveBlockerStart).not.toHaveBeenCalled();
    w.stop();
  });

  it("asks once a minute when the only run is one a crashed backend left running", async () => {
    runs = [{ id: "r1", started_at: new Date(Date.now() - STALE_RUN_MS - 60_000).toISOString() }];
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(polls).toBe(1);
    w.stop();
  });

  it("slows down on battery", async () => {
    runs = [{ id: "r1" }];
    const w = watcher(true);
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(polls).toBe(1);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(polls).toBe(2);
    w.stop();
  });

  it("polls at once when nudged, but not twice in a row", async () => {
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    w.nudge();
    await vi.advanceTimersByTimeAsync(0);
    expect(polls).toBe(1);

    await vi.advanceTimersByTimeAsync(5_000);
    w.nudge();
    await vi.advanceTimersByTimeAsync(0);
    expect(polls).toBe(2);
    // The nudge restarted the interval rather than adding a second timer.
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS - 1);
    expect(polls).toBe(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(polls).toBe(3);
    w.stop();
  });

  it("stops asking once stopped, including a poll already scheduled", async () => {
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    w.stop();
    w.nudge();
    await vi.advanceTimersByTimeAsync(10 * IDLE_POLL_INTERVAL_MS);
    expect(polls).toBe(1);
  });
});

/**
 * While the web app is on screen it polls the board itself, so the watcher
 * reads only the active runs — and the board when that list changed, which is
 * when a card can have moved under a run.
 */
describe("NotificationWatcher — reading the board while the page is on screen", () => {
  let tasks: RemoteTask[] = [];
  let runs: RemoteActiveRun[] = [];
  let onScreen = true;
  const reads = { tasks: 0, activity: 0, active: 0 };

  beforeEach(() => {
    tasks = [task({ id: "t1", key: "T-1", column: "todo" })];
    runs = [];
    onScreen = true;
    reads.tasks = reads.activity = reads.active = 0;
    shown.supported = true;
    shown.titles.length = 0;
    vi.useFakeTimers();
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        if (url.endsWith("/v1/tasks")) {
          reads.tasks += 1;
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ tasks }) });
        }
        if (url.endsWith("/v1/activity?limit=50")) {
          reads.activity += 1;
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ items: [] }) });
        }
        reads.active += 1;
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ runs: runs.length > 0 ? runs : null }) });
      }),
    );
    powerSaveBlockerStart.mockReset();
    powerSaveBlockerStart.mockReturnValue(5);
    powerSaveBlockerStop.mockReset();
  });

  afterEach(() => {
    shown.supported = false;
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  function watcher(): NotificationWatcher {
    return new NotificationWatcher({
      apiBase: () => "http://127.0.0.1:1",
      apiToken: () => "t",
      getPreferences: () => allOn(),
      onNotificationClick: () => {},
      isWindowFocused: () => false,
      isPageOnScreen: () => onScreen,
    });
  }

  const fresh = (id: string): RemoteActiveRun => ({ id, started_at: new Date().toISOString() });

  it("seeds once, then asks only for the active runs while nothing changes", async () => {
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(reads).toEqual({ tasks: 1, activity: 1, active: 1 });

    await vi.advanceTimersByTimeAsync(3 * IDLE_POLL_INTERVAL_MS);
    expect(reads).toEqual({ tasks: 1, activity: 1, active: 4 });
    w.stop();
  });

  it("reads the board on the two ticks after a run starts, and again after it ends", async () => {
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);

    runs = [fresh("r1")];
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(reads.tasks).toBe(2);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(reads.tasks).toBe(3);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(reads.tasks).toBe(3);

    runs = [];
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(reads.tasks).toBe(4);
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(reads.tasks).toBe(5);
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(reads.tasks).toBe(5);
    w.stop();
  });

  it("reads the board on every tick once nobody can see the page", async () => {
    onScreen = false;
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(2 * IDLE_POLL_INTERVAL_MS);
    expect(reads.tasks).toBe(3);
    w.stop();
  });

  it("still notifies a card that moved while the page was on screen, once the window is hidden", async () => {
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);

    tasks = [task({ id: "t1", key: "T-1", column: "human_uat" })];
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(shown.titles).toEqual([]);

    onScreen = false;
    await vi.advanceTimersByTimeAsync(IDLE_POLL_INTERVAL_MS);
    expect(shown.titles).toEqual(["T-1 — Awaiting your UAT"]);
    w.stop();
  });

  it("notifies a finished chat turn from the active runs alone, even when the board cannot be read", async () => {
    runs = [{ ...fresh("r1"), session_id: "s1", agent_id: "a1", title: "Chat" }];
    const w = watcher();
    w.start();
    await vi.advanceTimersByTimeAsync(0);

    runs = [];
    const answer = vi.mocked(fetch).getMockImplementation()!;
    vi.mocked(fetch).mockImplementation((url) =>
      String(url).endsWith("/v1/tasks")
        ? Promise.resolve({ ok: false, json: () => Promise.resolve({}) } as unknown as Response)
        : answer(url),
    );
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(shown.titles).toEqual(["Chat"]);
    w.stop();
  });
});
