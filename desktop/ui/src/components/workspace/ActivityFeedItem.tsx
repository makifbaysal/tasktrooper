import { Activity, ArrowRight, Bot, Radio } from "lucide-react";
import type { ActivityItem, BoardColumn } from "@/api";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { columnLabel, runStatusVariant } from "@/lib/project-board";
import { formatRelativeDate } from "@/lib/utils";
import { cn } from "@/lib/utils";

function payloadString(payload: Record<string, unknown> | undefined, key: string): string {
  const value = payload?.[key];
  return typeof value === "string" ? value : "";
}

interface ActivityFeedItemProps {
  item: ActivityItem;
  agentName?: string;
  taskLabel?: string;
  columns?: BoardColumn[];
  agentNameById?: Map<string, string>;
}

/** Renders the "what actually changed" line under a board event. */
function EventDetail({
  item,
  columns,
  agentNameById,
}: {
  item: ActivityItem;
  columns: BoardColumn[];
  agentNameById: Map<string, string>;
}) {
  const { t } = useI18n();
  const payload = item.payload;
  const agentLabel = (id: string) =>
    agentNameById.get(id) ?? t("chatArea.workspace.activityFeed.detail.unknownAgent");

  switch (item.event_type) {
    case "task.moved": {
      const from = payloadString(payload, "from_column");
      const to = payloadString(payload, "to_column");
      if (!from && !to) return null;
      const actorID = payloadString(payload, "actor_agent_id");
      return (
        <div className="mt-1.5 space-y-1">
          <div className="flex flex-wrap items-center gap-1.5 text-xs">
            <Badge variant="outline" className="font-normal">
              {columnLabel(from, columns)}
            </Badge>
            <ArrowRight className="h-3 w-3 text-muted-foreground" />
            <Badge variant="secondary" className="font-normal">
              {columnLabel(to, columns)}
            </Badge>
          </div>
          {actorID && (
            <p className="text-[11px] text-muted-foreground">
              {t("chatArea.workspace.activityFeed.detail.by", { name: agentLabel(actorID) })}
            </p>
          )}
        </div>
      );
    }
    case "task.assigned": {
      const from = payloadString(payload, "from_agent_id");
      const to = payloadString(payload, "to_agent_id");
      if (!from && !to) return null;
      return (
        <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs">
          <Badge variant="outline" className="font-normal">
            {from ? agentLabel(from) : t("chatArea.workspace.activityFeed.detail.unassigned")}
          </Badge>
          <ArrowRight className="h-3 w-3 text-muted-foreground" />
          <Badge variant="secondary" className="font-normal">
            {to ? agentLabel(to) : t("chatArea.workspace.activityFeed.detail.unassigned")}
          </Badge>
        </div>
      );
    }
    case "task.commented": {
      const authorName = payloadString(payload, "author_name");
      const authorType = payloadString(payload, "author_type");
      const authorID = payloadString(payload, "author_id");
      const content = payloadString(payload, "content");
      const author =
        authorName ||
        (authorType === "agent" && authorID ? agentLabel(authorID) : "") ||
        (authorType === "agent"
          ? t("chatArea.workspace.activityFeed.detail.unknownAgent")
          : // Verification bounces and clarification records are the control
            // plane's own comments; labelling them "User" credited the human
            // with reports they never wrote.
            authorType === "system"
            ? t("chatArea.workspace.activityFeed.detail.system")
            : t("chatArea.workspace.activityFeed.detail.user"));
      return (
        <div className="mt-1.5 space-y-1">
          <Badge variant="outline" className="font-normal text-xs">
            {author}
          </Badge>
          {content && <p className="line-clamp-2 text-xs text-muted-foreground">{content}</p>}
        </div>
      );
    }
    case "task.created": {
      const column = payloadString(payload, "column");
      const taskType = payloadString(payload, "task_type");
      const priority = payloadString(payload, "priority");
      const assignee = payloadString(payload, "assignee_agent_id");
      if (!column && !taskType && !priority && !assignee) return null;
      return (
        <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs">
          {column && (
            <Badge variant="secondary" className="font-normal">
              {columnLabel(column, columns)}
            </Badge>
          )}
          {taskType && (
            <Badge variant="outline" className="font-normal">
              {taskType}
            </Badge>
          )}
          {priority && (
            <Badge variant="outline" className="font-normal">
              {priority}
            </Badge>
          )}
          {assignee && (
            <Badge variant="outline" className="font-normal">
              {agentLabel(assignee)}
            </Badge>
          )}
        </div>
      );
    }
    default:
      return null;
  }
}

export function ActivityFeedItem({
  item,
  agentName,
  taskLabel,
  columns = [],
  agentNameById,
}: ActivityFeedItemProps) {
  const { t } = useI18n();
  const eventLabels: Record<string, string> = {
    "task.created": t("chatArea.workspace.activityFeed.event.taskCreated"),
    "task.moved": t("chatArea.workspace.activityFeed.event.taskMoved"),
    "task.commented": t("chatArea.workspace.activityFeed.event.taskCommented"),
    "task.assigned": t("chatArea.workspace.activityFeed.event.taskAssigned"),
  };
  const isRun = item.kind === "agent_run";

  return (
    <div className="rounded-lg border border-border bg-card px-3 py-2.5 transition-colors hover:bg-muted/20">
      <div className="flex items-start gap-2.5">
        <div
          className={cn(
            "mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md",
            isRun ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground",
          )}
        >
          {isRun ? <Bot className="h-3.5 w-3.5" /> : <Activity className="h-3.5 w-3.5" />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium">
              {isRun
                ? agentName ?? t("chatArea.workspace.activityFeed.agentRun")
                : eventLabels[item.event_type ?? ""] ?? item.event_type}
            </span>
            {item.status && (
              <Badge variant={runStatusVariant(item.status)} className="text-[10px]">
                {item.status}
              </Badge>
            )}
          </div>
          {taskLabel && <p className="mt-1 truncate text-xs text-muted-foreground">{taskLabel}</p>}
          {!isRun && (
            <EventDetail item={item} columns={columns} agentNameById={agentNameById ?? new Map()} />
          )}
          {item.summary && (
            <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{item.summary}</p>
          )}
          <p className="mt-1.5 text-[11px] text-muted-foreground">{formatRelativeDate(item.created_at)}</p>
        </div>
      </div>
    </div>
  );
}

interface ActivityFeedHeaderProps {
  live?: boolean;
}

export function ActivityFeedHeader({ live }: ActivityFeedHeaderProps) {
  const { t } = useI18n();
  return (
    <div className="flex items-center justify-between border-b border-border px-4 py-3">
      <div className="flex items-center gap-2 font-medium">
        <Activity className="h-4 w-4 text-muted-foreground" />
        {t("chatArea.workspace.activityFeed.title")}
      </div>
      {live && (
        <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Radio className="h-3 w-3 animate-pulse text-success" />
          {t("chatArea.workspace.activityFeed.live")}
        </span>
      )}
    </div>
  );
}
