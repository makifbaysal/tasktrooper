import { useEffect, useState, type ReactNode } from "react";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import type { ActivityFeed, FeedCurrent } from "@/lib/activityFeed";
import { currentLabel, formatElapsed, statusLabel } from "@/lib/activityFeedLabels";
import { runStatusVariant } from "@/lib/project-board";
import { formatCompact } from "@/lib/usage";

interface AgentRunHeaderProps {
  agentName: string;
  lead?: boolean;
  runStatus: string;
  live: boolean;
  startedAt?: string;
  endedAt?: string;
  current: FeedCurrent | null;
  stats: ActivityFeed["stats"];
  tokens?: number;
  actions?: ReactNode;
}

function useElapsed(live: boolean, startedAt?: string, endedAt?: string): string | null {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!live) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [live]);
  const start = startedAt ? Date.parse(startedAt) : NaN;
  if (!Number.isFinite(start)) return null;
  if (live) return formatElapsed(now - start);
  const end = endedAt ? Date.parse(endedAt) : NaN;
  return Number.isFinite(end) ? formatElapsed(end - start) : null;
}

export function AgentRunHeader({
  agentName,
  lead = false,
  runStatus,
  live,
  startedAt,
  endedAt,
  current,
  stats,
  tokens,
  actions,
}: AgentRunHeaderProps) {
  const { t } = useI18n();
  const elapsed = useElapsed(live, startedAt, endedAt);

  const laneChip =
    current && current.laneTitle !== undefined
      ? current.laneAgent || current.laneTitle || t("activityArea.lane.subagent")
      : null;
  const label = currentLabel(t, current);
  const parallel = current?.parallel ?? 0;

  const facts = [
    stats.toolCalls > 0 && t("activityArea.stats.tools", { count: stats.toolCalls }),
    stats.failures > 0 && t("activityArea.stats.failures", { count: stats.failures }),
    stats.subagents > 0 && t("activityArea.stats.subagents", { count: stats.subagents }),
    tokens && tokens > 0 ? t("activityArea.stats.tokens", { value: formatCompact(tokens) }) : false,
    stats.costUsd && stats.costUsd > 0 ? `$${stats.costUsd.toFixed(2)}` : false,
    stats.model || false,
  ].filter((part): part is string => typeof part === "string");

  return (
    <Card className="min-w-0 space-y-2.5 p-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="flex min-w-[10rem] flex-1 items-center gap-2.5">
          <AgentAvatar name={agentName} lead={lead} size="md" />
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="min-w-0 max-w-full truncate text-body font-semibold" title={agentName}>
              {agentName}
            </span>
            <Badge variant={runStatusVariant(runStatus)} className="shrink-0 gap-1.5">
              {live && <span aria-hidden className="h-1.5 w-1.5 animate-pulse rounded-full bg-current" />}
              {statusLabel(t, runStatus)}
            </Badge>
            {elapsed && <span className="shrink-0 text-caption tabular-nums text-muted-foreground">{elapsed}</span>}
          </div>
        </div>
        {actions && <div className="flex shrink-0 items-center gap-1">{actions}</div>}
      </div>

      {live && (
        <div
          className="flex min-w-0 items-center gap-2 rounded-md bg-muted/60 px-2.5 py-1.5 text-body"
          title={laneChip ? `${laneChip} › ${label}` : label}
        >
          <Spinner size="sm" className="h-3.5 w-3.5 shrink-0 text-warning" />
          {laneChip && (
            <span className="shrink-0 rounded bg-background px-1.5 py-0.5 text-micro font-medium text-muted-foreground">
              {laneChip} ›
            </span>
          )}
          <span className="min-w-0 flex-1 truncate">{label}</span>
          {parallel > 0 && (
            <span className="shrink-0 text-micro text-muted-foreground">
              {t("activityArea.current.parallel", { count: parallel })}
            </span>
          )}
        </div>
      )}

      {facts.length > 0 && (
        <p className="text-caption text-muted-foreground [overflow-wrap:anywhere]">{facts.join(" · ")}</p>
      )}
    </Card>
  );
}
