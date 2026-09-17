import { describe, expect, it } from "vitest";
import type { NotificationPreferences } from "../../ipc/types.js";
import { diffForNotifications, type RemoteActivityItem, type RemoteTask } from "./notifications.js";

function allOn(): NotificationPreferences {
  return { enabled: true, analizReview: true, humanUat: true, humanNeeded: true, agentComments: true };
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
