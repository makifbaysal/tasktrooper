import { Activity } from "lucide-react";
import { useCallback, useState } from "react";
import type { SessionStep } from "@/api";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import type { ActivityFeed as ActivityFeedModel } from "@/lib/activityFeed";
import { currentLabel, laneTitle } from "@/lib/activityFeedLabels";
import { cn } from "@/lib/utils";
import { FeedItemList, type FeedFocus } from "./FeedItemList";
import { FeedStatusIcon } from "./FeedStatusIcon";
import { RawStepList } from "./RawStepList";

interface ActivityFeedProps {
  feed: ActivityFeedModel;
  live: boolean;
  agentNameMap?: Record<string, string>;
  rawSteps?: SessionStep[];
  emptyLabel?: string;
  className?: string;
}

export function ActivityFeed({ feed, live, agentNameMap, rawSteps, emptyLabel, className }: ActivityFeedProps) {
  const { t } = useI18n();
  const [focus, setFocus] = useState<FeedFocus | null>(null);
  const [rawOpen, setRawOpen] = useState(false);

  const jumpTo = useCallback((id: string) => setFocus((prev) => ({ id, nonce: (prev?.nonce ?? 0) + 1 })), []);
  const ctx = { plan: feed.plan, agentNameMap, focus };
  const empty = feed.items.length === 0;

  return (
    <div className={cn("min-w-0 space-y-3", className)}>
      {feed.lanes.length > 0 && (
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <span className="text-micro font-medium text-muted-foreground">{t("activityArea.feed.subagents")}</span>
          {feed.lanes.map((lane) => {
            const title = laneTitle(t, lane);
            const agent = lane.agentName ? (agentNameMap?.[lane.agentName] ?? lane.agentName) : null;
            return (
              <Button
                key={lane.id}
                type="button"
                variant="outline"
                size="sm"
                className="h-7 min-w-0 max-w-full gap-1.5 px-2 text-caption font-normal [&_svg]:size-3.5"
                aria-label={t("activityArea.feed.jumpTo", { title })}
                onClick={() => jumpTo(lane.id)}
              >
                <FeedStatusIcon status={lane.status} />
                <span className="min-w-0 truncate">{title}</span>
                {agent && agent !== title && <span className="hidden truncate text-muted-foreground sm:inline">{agent}</span>}
              </Button>
            );
          })}
        </div>
      )}

      {empty && !live ? (
        <EmptyState icon={Activity} title={emptyLabel ?? t("activityArea.runs.noSteps")} className="py-8" />
      ) : (
        <FeedItemList items={feed.items} ctx={ctx} />
      )}

      {live && (
        <div className="flex min-w-0 items-center gap-2 pl-1 text-caption text-muted-foreground">
          <Spinner size="sm" className="h-3.5 w-3.5 shrink-0 text-warning" />
          <span className="min-w-0 truncate">{currentLabel(t, feed.current)}</span>
        </div>
      )}

      {rawSteps && rawSteps.length > 0 && (
        <div className="space-y-2 border-t border-border pt-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-micro font-normal text-muted-foreground"
            aria-expanded={rawOpen}
            onClick={() => setRawOpen((v) => !v)}
          >
            {t("activityArea.feed.rawSteps", { count: rawSteps.length })}
          </Button>
          {rawOpen && <RawStepList steps={rawSteps} />}
        </div>
      )}
    </div>
  );
}
