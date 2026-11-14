import type { Agent, ActivityItem, BoardTask } from "@/api";
import { analysisReviewPath } from "@/lib/analysis-review";

export interface NotificationItem {
  id: string;
  taskId: string;
  agentId?: string;
  sessionId?: string;
  kind: "comment" | "chat" | "review" | "uat" | "decision" | "question";
  /** Set for "comment"/"chat": the sender's name, shown instead of a generic icon+title. */
  authorName?: string;
  /** Set for "review"/"uat"/"decision"/"question": a `t()` key + its `{task}` interpolation, resolved at render time so this stays a pure, localization-free function. */
  titleKey?: string;
  titleVars?: Record<string, string | number>;
  body: string;
  createdAt: string;
  /** Overrides the default `/board?task=<id>` destination — an analysis_questions park routes to the report instead of a chat that does not exist. */
  path?: string;
}

export interface ReadCursor {
  lastEventId: string;
  lastEventAt: string;
}

const BODY_MAX = 140;

function payloadString(payload: Record<string, unknown> | undefined, key: string): string {
  const value = payload?.[key];
  return typeof value === "string" ? value : "";
}

function truncate(s: string, max = BODY_MAX): string {
  if (s.length <= max) return s;
  return `${s.slice(0, max - 1).trimEnd()}…`;
}

/**
 * `task.commented`'in aktörü `payload.author_id`'de durur, `task.moved`'ınki
 * ise taşınan task'ın kendi `assignee_agent_id`'sinde — ikisi de aynı
 * `agentId` alanına toplanır ki sidebar'daki tek "seen" haritası ikisini de
 * besleyebilsin (bkz. A-16 spec, sidebar rozeti bölümü).
 */
export function agentIdForItem(item: ActivityItem, tasks: BoardTask[]): string | undefined {
  if (item.event_type === "task.commented") {
    return payloadString(item.payload, "author_id") || undefined;
  }
  if (item.event_type === "task.moved") {
    return tasks.find((task) => task.id === item.task_id)?.assignee_agent_id;
  }
  return undefined;
}

function compareByCreatedAtDesc(a: NotificationItem, b: NotificationItem): number {
  if (a.createdAt !== b.createdAt) return a.createdAt > b.createdAt ? -1 : 1;
  return a.id > b.id ? -1 : a.id < b.id ? 1 : 0;
}

/**
 * Mirrors `diffForNotifications`'s "which event deserves attention" rule
 * (`desktop/src/main/services/notifications.ts:131-144`) in the renderer,
 * without importing across the Electron/browser boundary — the two packages
 * do not share code (see A-16 spec, option #1).
 */
export function toNotificationItems(
  activity: ActivityItem[],
  tasks: BoardTask[],
  agents: Agent[] = [],
): NotificationItem[] {
  const taskById = new Map(tasks.map((task) => [task.id, task]));
  const agentById = new Map(agents.map((agent) => [agent.id, agent]));
  const result: NotificationItem[] = [];

  for (const item of activity) {
    if (item.kind === "chat_message") {
      if (!item.agent_id) continue;
      result.push({
        id: item.id,
        taskId: item.task_id ?? "",
        agentId: item.agent_id,
        sessionId: item.session_id,
        kind: "chat",
        authorName: agentById.get(item.agent_id)?.name ?? "An agent",
        body: truncate(item.summary ?? ""),
        createdAt: item.created_at,
        path:
          item.session_id
            ? `/agents/${encodeURIComponent(item.agent_id)}/chat/${encodeURIComponent(item.session_id)}`
            : undefined,
      });
      continue;
    }

    if (item.kind !== "board_event") continue;
    const task = item.task_id ? taskById.get(item.task_id) : undefined;
    const label = task?.key ?? "A task";

    if (item.event_type === "task.commented") {
      if (payloadString(item.payload, "author_type") !== "agent") continue;
      const authorName = payloadString(item.payload, "author_name") || "An agent";
      const content = payloadString(item.payload, "content");
      result.push({
        id: item.id,
        taskId: item.task_id ?? "",
        agentId: agentIdForItem(item, tasks),
        kind: "comment",
        authorName,
        body: truncate(content),
        createdAt: item.created_at,
      });
      continue;
    }

    if (item.event_type === "task.moved") {
      const toColumn = payloadString(item.payload, "to_column");
      const base = {
        id: item.id,
        taskId: item.task_id ?? "",
        agentId: agentIdForItem(item, tasks),
        body: truncate(task?.title ?? ""),
        createdAt: item.created_at,
        titleVars: { task: label },
      };
      if (toColumn === "analiz_review") {
        result.push({ ...base, kind: "review", titleKey: "frame.layout.header.notificationCenter.items.review" });
      } else if (toColumn === "human_uat") {
        result.push({ ...base, kind: "uat", titleKey: "frame.layout.header.notificationCenter.items.uat" });
      } else if (toColumn === "blocked") {
        if (task?.blocked_resource === "analysis_questions") {
          result.push({
            ...base,
            kind: "question",
            titleKey: "frame.layout.header.notificationCenter.items.question",
            path: task.repository_id ? analysisReviewPath(task.repository_id, task.id) : undefined,
          });
        } else if (task?.blocked_resource === "human_decision") {
          result.push({ ...base, kind: "decision", titleKey: "frame.layout.header.notificationCenter.items.decision" });
        } else if (task?.blocked_question) {
          result.push({ ...base, kind: "question", titleKey: "frame.layout.header.notificationCenter.items.question" });
        }
      }
    }
  }

  return result.sort(compareByCreatedAtDesc);
}

export function cursorFor(item: NotificationItem): ReadCursor {
  return { lastEventId: item.id, lastEventAt: item.createdAt };
}

function isAfterCursor(item: NotificationItem, cursor: ReadCursor): boolean {
  if (item.createdAt !== cursor.lastEventAt) return item.createdAt > cursor.lastEventAt;
  return item.id !== cursor.lastEventId;
}

export function partitionByCursor(
  items: NotificationItem[],
  cursor: ReadCursor | null,
): { unread: NotificationItem[]; read: NotificationItem[] } {
  if (!cursor) return { unread: [], read: items };
  const unread: NotificationItem[] = [];
  const read: NotificationItem[] = [];
  for (const item of items) {
    (isAfterCursor(item, cursor) ? unread : read).push(item);
  }
  return { unread, read };
}

export function laterCursor(a: ReadCursor | null, b: ReadCursor | null): ReadCursor | null {
  if (!a) return b;
  if (!b) return a;
  if (a.lastEventAt !== b.lastEventAt) return a.lastEventAt > b.lastEventAt ? a : b;
  return a.lastEventId >= b.lastEventId ? a : b;
}

export function latestCursor(items: NotificationItem[]): ReadCursor | null {
  return items.reduce<ReadCursor | null>((best, item) => laterCursor(best, cursorFor(item)), null);
}
