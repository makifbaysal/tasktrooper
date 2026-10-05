import { stepLabel } from "@/lib/planUtils";
import { stripMcpPrefix, type FeedCurrent, type FeedItem, type FeedLane, type FeedTool, type ToolKind } from "@/lib/activityFeed";

export type Translate = (key: string, params?: Record<string, string | number>) => string;

type ToolsItem = Extract<FeedItem, { kind: "tools" }>;
type EventItem = Extract<FeedItem, { kind: "event" }>;

export function humanizeToolName(name: string): string {
  const spaced = stripMcpPrefix(name).replace(/_+/g, " ").trim();
  return spaced ? spaced.charAt(0).toUpperCase() + spaced.slice(1) : name;
}

const TARGETED: ReadonlySet<ToolKind> = new Set(["read", "search", "edit", "write", "shell", "browser", "web"]);

function toolFormKind(kind: ToolKind, target: string | undefined): ToolKind {
  if (kind === "subagent") return "other";
  if (TARGETED.has(kind) && !target) return "other";
  return kind;
}

export function toolLabel(t: Translate, tool: FeedTool): string {
  const kind = toolFormKind(tool.kind, tool.target);
  const form = tool.status === "running" || tool.status === "pending" ? "running" : "done";
  return t(`activityArea.tool.${kind}.${form}`, {
    target: tool.target ?? "",
    name: tool.target ?? humanizeToolName(tool.name),
  });
}

export function toolGroupLabel(t: Translate, item: ToolsItem): string {
  if (item.tools.length === 1) return toolLabel(t, item.tools[0]);
  const kind = item.toolKind === "subagent" ? "other" : item.toolKind;
  return t(`activityArea.tool.${kind}.many`, { count: item.tools.length });
}

export function currentLabel(t: Translate, current: FeedCurrent | null): string {
  if (!current) return t("activityArea.current.thinking");
  if (current.kind === "tool" && current.tool) return toolLabel(t, current.tool);
  return t(`activityArea.current.${current.kind === "tool" ? "thinking" : current.kind}`);
}

const UNRESOLVED_GROUP = /\s*\([^()]*\{\w+\}[^()]*\)/g;
const UNRESOLVED_VAR = /\{\w+\}/g;

export function eventLabel(t: Translate, item: EventItem): string {
  const payload = item.payload;
  let type = item.stepType;
  if (type === "verification_complete") type = payload.passed === false ? "verification_failed" : "verification_passed";
  const key = `activityArea.event.${type}`;
  const params: Record<string, string | number> = {};
  const count = payload.task_count;
  if (typeof count === "number") params.count = count;
  if (typeof payload.open === "number") params.open = payload.open;
  if (typeof payload.resource === "string") params.resource = payload.resource;
  const label = t(key, params);
  if (label === key) return stepLabel(item.stepType);
  return label.replace(UNRESOLVED_GROUP, "").replace(UNRESOLVED_VAR, "").trim();
}

export function laneTitle(t: Translate, lane: FeedLane): string {
  if (lane.title) return lane.title;
  if (lane.source === "subtask") return lane.id.replace(/^task:/, "").replace(/@.*$/, "");
  return lane.agentName || t("activityArea.lane.subagent");
}

export function laneTag(t: Translate, lane: FeedLane): string {
  return t(lane.source === "subtask" ? "activityArea.lane.subtask" : "activityArea.lane.subagent");
}

export function statusLabel(t: Translate, runStatus: string): string {
  const key = `activityArea.status.${runStatus.toLowerCase()}`;
  const label = t(key);
  return label === key ? runStatus : label;
}

export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const ss = String(s).padStart(2, "0");
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${ss}`;
  return `${m}:${ss}`;
}

export function formatToolDuration(ms: number): string {
  if (ms < 1000) return "<1s";
  const total = Math.round(ms / 1000);
  if (total < 60) return `${total}s`;
  const m = Math.floor(total / 60);
  if (m < 60) return `${m}m ${total % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

export function durationBetween(startedAt: string | undefined, endedAt: string | undefined): number | null {
  if (!startedAt || !endedAt) return null;
  const ms = Date.parse(endedAt) - Date.parse(startedAt);
  return Number.isFinite(ms) && ms >= 0 ? ms : null;
}

export function formatClock(iso: string, lang: string, withSeconds = false): string {
  const time = Date.parse(iso);
  if (!Number.isFinite(time)) return "";
  return new Date(time).toLocaleTimeString(lang === "tr" ? "tr-TR" : "en-US", {
    hour: "2-digit",
    minute: "2-digit",
    ...(withSeconds ? { second: "2-digit" } : {}),
  });
}

export function prettyJson(raw: string | undefined): string {
  if (!raw) return "";
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}
