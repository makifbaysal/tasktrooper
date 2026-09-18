import { describe, expect, it } from "vitest";
import type { ActivityItem, BoardTask } from "@/api";
import {
  agentIdForItem,
  cursorFor,
  latestCursor,
  partitionByCursor,
  toNotificationItems,
} from "@/lib/notifications";

function task(overrides: Partial<BoardTask> = {}): BoardTask {
  return {
    id: "task-1",
    repository_id: "repo-1",
    key: "T-1",
    task_number: 1,
    title: "Fix the thing",
    task_type: "task",
    description: "",
    technical_description: "",
    column: "in_progress",
    position: 0,
    priority: "medium",
    created_by: "human",
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}

function commentItem(overrides: Partial<ActivityItem> = {}): ActivityItem {
  return {
    id: "evt-1",
    kind: "board_event",
    task_id: "task-1",
    event_type: "task.commented",
    payload: { author_type: "agent", author_id: "agent-1", author_name: "Rex", content: "Done." },
    created_at: "2026-09-18T10:00:00Z",
    ...overrides,
  };
}

function movedItem(overrides: Partial<ActivityItem> = {}): ActivityItem {
  return {
    id: "evt-2",
    kind: "board_event",
    task_id: "task-1",
    event_type: "task.moved",
    payload: { from_column: "in_progress", to_column: "analiz_review" },
    created_at: "2026-09-18T10:05:00Z",
    ...overrides,
  };
}

describe("toNotificationItems", () => {
  it("includes an agent comment", () => {
    const items = toNotificationItems([commentItem()], [task()]);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ id: "evt-1", taskId: "task-1", agentId: "agent-1", kind: "comment" });
  });

  it("excludes a human or system comment", () => {
    const human = commentItem({ id: "evt-3", payload: { author_type: "user", content: "hi" } });
    const system = commentItem({ id: "evt-4", payload: { author_type: "system", content: "hi" } });
    expect(toNotificationItems([human, system], [task()])).toHaveLength(0);
  });

  it("includes a move to analiz_review or human_uat", () => {
    const uat = movedItem({ id: "evt-5", payload: { from_column: "in_progress", to_column: "human_uat" } });
    const items = toNotificationItems([movedItem(), uat], [task()]);
    expect(items.map((i) => i.kind).sort()).toEqual(["review", "uat"]);
  });

  it("distinguishes a blocked human_decision from a blocked question", () => {
    const decisionTask = task({ id: "task-2", blocked_resource: "human_decision" });
    const decisionMove = movedItem({
      id: "evt-6",
      task_id: "task-2",
      payload: { from_column: "in_progress", to_column: "blocked" },
    });
    const questionTask = task({ id: "task-3", blocked_question: "Which branch?" });
    const questionMove = movedItem({
      id: "evt-7",
      task_id: "task-3",
      payload: { from_column: "in_progress", to_column: "blocked" },
    });
    const items = toNotificationItems([decisionMove, questionMove], [decisionTask, questionTask]);
    expect(items.find((i) => i.id === "evt-6")?.kind).toBe("decision");
    expect(items.find((i) => i.id === "evt-7")?.kind).toBe("question");
  });

  it("excludes a blocked move with neither a decision nor a question", () => {
    const plainTask = task({ id: "task-4" });
    const plainMove = movedItem({
      id: "evt-8",
      task_id: "task-4",
      payload: { from_column: "in_progress", to_column: "blocked" },
    });
    expect(toNotificationItems([plainMove], [plainTask])).toHaveLength(0);
  });

  it("excludes routine events: task.created, task.assigned, agent_run, non-gate task.moved", () => {
    const created: ActivityItem = {
      id: "evt-9",
      kind: "board_event",
      task_id: "task-1",
      event_type: "task.created",
      created_at: "2026-09-18T10:00:00Z",
    };
    const assigned: ActivityItem = {
      id: "evt-10",
      kind: "board_event",
      task_id: "task-1",
      event_type: "task.assigned",
      created_at: "2026-09-18T10:00:00Z",
    };
    const agentRun: ActivityItem = {
      id: "evt-11",
      kind: "agent_run",
      task_id: "task-1",
      created_at: "2026-09-18T10:00:00Z",
    };
    const routineMove = movedItem({
      id: "evt-12",
      payload: { from_column: "todo", to_column: "in_progress" },
    });
    expect(toNotificationItems([created, assigned, agentRun, routineMove], [task()])).toHaveLength(0);
  });

  it("sorts newest first regardless of input order", () => {
    const older = commentItem({ id: "evt-old", created_at: "2026-09-18T09:00:00Z" });
    const newer = commentItem({ id: "evt-new", created_at: "2026-09-18T11:00:00Z" });
    const items = toNotificationItems([older, newer], [task()]);
    expect(items.map((i) => i.id)).toEqual(["evt-new", "evt-old"]);
  });
});

describe("agentIdForItem", () => {
  it("reads payload.author_id for task.commented", () => {
    expect(agentIdForItem(commentItem(), [task()])).toBe("agent-1");
  });

  it("reads the task's assignee_agent_id for task.moved", () => {
    const t = task({ assignee_agent_id: "agent-2" });
    expect(agentIdForItem(movedItem(), [t])).toBe("agent-2");
  });
});

describe("partitionByCursor", () => {
  it("treats every item as read when the cursor is null (first install seed)", () => {
    const items = toNotificationItems([commentItem(), movedItem()], [task()]);
    const { unread, read } = partitionByCursor(items, null);
    expect(unread).toHaveLength(0);
    expect(read).toHaveLength(items.length);
    expect(latestCursor(items)).toEqual(cursorFor(items[0]));
  });

  it("splits items after the cursor as unread, at-or-before as read", () => {
    const items = toNotificationItems([commentItem(), movedItem()], [task()]);
    const cursor = cursorFor(items[1]); // the older item (task.commented at 10:00)
    const { unread, read } = partitionByCursor(items, cursor);
    expect(unread.map((i) => i.id)).toEqual(["evt-2"]);
    expect(read.map((i) => i.id)).toEqual(["evt-1"]);
  });

  it("breaks a tie on identical created_at by id, same as the desktop watcher", () => {
    const a: ActivityItem = { ...commentItem(), id: "evt-a" };
    const b: ActivityItem = { ...commentItem(), id: "evt-b" };
    const items = toNotificationItems([a, b], [task()]);
    const cursor = cursorFor(items.find((i) => i.id === "evt-a")!);
    const { unread, read } = partitionByCursor(items, cursor);
    expect(unread.map((i) => i.id)).toEqual(["evt-b"]);
    expect(read.map((i) => i.id)).toEqual(["evt-a"]);
  });
});
