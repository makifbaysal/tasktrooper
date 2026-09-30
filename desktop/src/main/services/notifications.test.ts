import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NotificationPreferences } from "../../ipc/types.js";
import {
  type ActiveChatSnapshot,
  diffChatCompletions,
  diffForNotifications,
  hasActiveRun,
  NotificationWatcher,
  POLL_INTERVAL_MS,
  type RemoteActiveRun,
  type RemoteActivityItem,
  type RemoteTask,
} from "./notifications.js";

const powerSaveBlockerStart = vi.fn<(type: string) => number>();
const powerSaveBlockerStop = vi.fn<(id: number) => void>();

vi.mock("electron", () => ({
  Notification: class {
    static isSupported(): boolean {
      return false;
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
  let activeRuns: Array<{ id: string }> = [];
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
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);

    expect(tasksPolled).toBeGreaterThanOrEqual(3);
    expect(powerSaveBlockerStart).not.toHaveBeenCalledWith("prevent-app-suspension");
    watcher.stop();
  });
});
