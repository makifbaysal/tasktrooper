import { ChevronDown, ChevronRight, CircleStop, Loader2, RotateCcw } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import type { Agent, BoardTask, TaskAgentRun } from "@/api";
import { ActivityFeed } from "@/components/activity/ActivityFeed";
import { AgentRunHeader } from "@/components/activity/AgentRunHeader";
import { FeedStatusIcon } from "@/components/activity/FeedStatusIcon";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useRunActivity } from "@/hooks/useRunActivity";
import { useStickToBottom } from "@/hooks/useStickToBottom";
import { buildActivityFeed, type FeedStatus } from "@/lib/activityFeed";
import { LEAD_CATALOG_SLUG } from "@/lib/leadAgent";
import { formatCompact } from "@/lib/usage";
import { formatRelativeDate } from "@/lib/utils";

// The server only accepts a stop while the run is still open, and a rerun only
// once it has settled — mirroring that here keeps a doomed request off the wire.
const STOPPABLE_RUN_STATUSES = new Set(["pending", "running"]);
const RERUNNABLE_RUN_STATUSES = new Set(["completed", "failed", "cancelled"]);

// prompt keeps the server's contract: it is the TOTAL prompt size, with the
// cache counters as subsets of it — total spend is prompt + completion.
interface TokenTally {
  prompt: number;
  completion: number;
  cacheRead: number;
  cacheWrite: number;
}

function listStatus(status: string): FeedStatus {
  switch (status) {
    case "running":
      return "running";
    case "completed":
      return "completed";
    case "failed":
      return "failed";
    case "pending":
      return "pending";
    default:
      return "incomplete";
  }
}

const totalTokens = (run: TaskAgentRun) => (run.prompt_tokens ?? 0) + (run.completion_tokens ?? 0);

interface TaskAgentRunsSectionProps {
  task: BoardTask;
  runs: TaskAgentRun[];
  agents: Agent[];
  agentNameMap: Record<string, string>;
  /** The drawer is open; nothing is polled while it is not. */
  active: boolean;
  runActionId: string | null;
  onStop: (runId: string) => void;
  onRerun: (runId: string) => void;
}

export function TaskAgentRunsSection({
  task,
  runs,
  agents,
  agentNameMap,
  active,
  runActionId,
  onStop,
  onRerun,
}: TaskAgentRunsSectionProps) {
  const { t } = useI18n();
  const [pickedId, setPickedId] = useState<string | null>(null);
  const [tokensOpen, setTokensOpen] = useState(false);

  const sorted = useMemo(
    () => [...runs].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at)),
    [runs],
  );
  const shown =
    sorted.find((run) => run.id === pickedId) ??
    sorted.find((run) => STOPPABLE_RUN_STATUSES.has(run.status)) ??
    sorted[0] ??
    null;
  const others = shown ? sorted.filter((run) => run.id !== shown.id) : [];

  const agentOf = (run: TaskAgentRun) => agents.find((a) => a.id === run.agent_id);
  const agentName = (run: TaskAgentRun) =>
    agentOf(run)?.name ?? t("boardArea.components.taskDetail.agentFallback");

  const tokenTotals = useMemo(() => {
    const byAgent = new Map<string, TokenTally>();
    for (const run of runs) {
      if (totalTokens(run) === 0) continue;
      const acc = byAgent.get(run.agent_id) ?? { prompt: 0, completion: 0, cacheRead: 0, cacheWrite: 0 };
      acc.prompt += run.prompt_tokens ?? 0;
      acc.completion += run.completion_tokens ?? 0;
      acc.cacheRead += run.cache_read_tokens ?? 0;
      acc.cacheWrite += run.cache_write_tokens ?? 0;
      byAgent.set(run.agent_id, acc);
    }
    return [...byAgent.entries()];
  }, [runs]);

  const tokenLine = (u: TokenTally) => {
    const parts = [
      `${formatCompact(u.prompt)} ${t("boardArea.components.taskDetail.tokenIn")}`,
      `${formatCompact(u.completion)} ${t("boardArea.components.taskDetail.tokenOut")}`,
    ];
    if (u.cacheRead > 0) parts.push(`${formatCompact(u.cacheRead)} ${t("boardArea.components.taskDetail.tokenCacheRead")}`);
    if (u.cacheWrite > 0) parts.push(`${formatCompact(u.cacheWrite)} ${t("boardArea.components.taskDetail.tokenCacheWrite")}`);
    parts.push(`${formatCompact(u.prompt + u.completion)} ${t("boardArea.components.taskDetail.tokenTotal")}`);
    return parts.join(" · ");
  };

  const sessionRunId = shown?.session_run_id ?? null;
  const { steps, plan, isLive, loading } = useRunActivity(sessionRunId, active && !!shown, shown?.status);
  const feed = useMemo(
    () =>
      buildActivityFeed([
        { runId: sessionRunId ?? "", live: isLive, startedAt: shown?.created_at, steps, plan },
      ]),
    [sessionRunId, isLive, shown?.created_at, steps, plan],
  );

  const scrollRef = useRef<HTMLDivElement>(null);
  useStickToBottom(scrollRef, [steps.length, feed.current?.kind, isLive]);

  const liveRow = !!shown && STOPPABLE_RUN_STATUSES.has(shown.status);
  const busy = !!shown && runActionId === shown.id;
  // A blocked task is waiting on the human, so handing it a fresh run would
  // only bounce off the server's gate.
  const canRerun = !!shown && !task.blocked_at && RERUNNABLE_RUN_STATUSES.has(shown.status);

  const actions = shown ? (
    <>
      {liveRow && (
        <Button type="button" variant="outline" size="sm" className="gap-1.5" disabled={busy} onClick={() => onStop(shown.id)}>
          {busy ? <Loader2 className="animate-spin" /> : <CircleStop />}
          {t("boardArea.components.taskDetail.runStop")}
        </Button>
      )}
      {canRerun && (
        <Button type="button" variant="outline" size="sm" className="gap-1.5" disabled={busy} onClick={() => onRerun(shown.id)}>
          {busy ? <Loader2 className="animate-spin" /> : <RotateCcw />}
          {t("boardArea.components.taskDetail.runRerun")}
        </Button>
      )}
    </>
  ) : null;

  const waiting = !!shown && !sessionRunId;
  const firstLoad = !!sessionRunId && loading && steps.length === 0;
  const finished = !!shown && !liveRow;
  const TokensChevron = tokensOpen ? ChevronDown : ChevronRight;

  return (
    <section className="min-w-0 space-y-3">
      <Label className="text-muted-foreground">
        {t("boardArea.components.taskDetail.agentRuns", { count: runs.length })}
      </Label>

      {!shown ? (
        <p className="text-caption text-muted-foreground">{t("boardArea.components.taskDetail.noRuns")}</p>
      ) : (
        <div className="min-w-0 space-y-3">
          <AgentRunHeader
            agentName={agentName(shown)}
            lead={agentOf(shown)?.catalog_slug === LEAD_CATALOG_SLUG}
            runStatus={shown.status}
            live={liveRow && !waiting}
            startedAt={shown.created_at}
            endedAt={finished ? shown.updated_at : undefined}
            current={feed.current}
            stats={feed.stats}
            tokens={totalTokens(shown)}
            actions={actions}
          />

          {finished && shown.summary && (
            <p className="line-clamp-4 text-caption text-muted-foreground [overflow-wrap:anywhere]">{shown.summary}</p>
          )}

          {waiting ? (
            <p className="text-caption text-muted-foreground">{t("activityArea.runs.queued")}</p>
          ) : firstLoad ? (
            <Card className="space-y-2.5 p-3" aria-busy="true" aria-label={t("boardArea.components.taskDetail.runLoading")}>
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-5/6" />
            </Card>
          ) : (
            <Card className="min-w-0 p-3 shadow-none">
              <div ref={scrollRef} className="max-h-[560px] min-w-0 overflow-y-auto pr-1">
                <ActivityFeed
                  feed={feed}
                  live={isLive}
                  agentNameMap={agentNameMap}
                  rawSteps={steps}
                  emptyLabel={t("activityArea.runs.noSteps")}
                />
              </div>
            </Card>
          )}
        </div>
      )}

      {others.length > 0 && (
        <div className="min-w-0 space-y-1.5">
          <p className="text-caption font-medium text-muted-foreground">
            {t("activityArea.runs.other", { count: others.length })}
          </p>
          <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border">
            {others.map((run) => {
              const tokens = totalTokens(run);
              return (
                <li key={run.id} className="min-w-0">
                  <Button
                    type="button"
                    variant="ghost"
                    className="h-auto w-full min-w-0 items-start justify-start gap-2.5 whitespace-normal rounded-none px-3 py-2 text-left font-normal"
                    onClick={() => setPickedId(run.id)}
                  >
                    <FeedStatusIcon status={listStatus(run.status)} className="mt-1" />
                    <span className="min-w-0 flex-1">
                      <span className="flex min-w-0 items-baseline gap-2">
                        <span className="min-w-0 truncate text-caption font-medium">{agentName(run)}</span>
                        <span className="shrink-0 text-micro text-muted-foreground">{formatRelativeDate(run.created_at)}</span>
                      </span>
                      {run.summary && (
                        <span className="block truncate text-micro text-muted-foreground">{run.summary}</span>
                      )}
                    </span>
                    {tokens > 0 && (
                      <span className="shrink-0 text-micro tabular-nums text-muted-foreground">
                        {formatCompact(tokens)}
                      </span>
                    )}
                  </Button>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {tokenTotals.length > 0 && (
        <div className="min-w-0">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 gap-1 px-2 text-caption font-normal text-muted-foreground [&_svg]:size-3.5"
            aria-expanded={tokensOpen}
            onClick={() => setTokensOpen((v) => !v)}
          >
            <TokensChevron aria-hidden />
            {t("boardArea.components.taskDetail.tokenUsage")}
          </Button>
          {tokensOpen && (
            <div className="mt-1 space-y-1 rounded-lg border border-border bg-muted/10 px-3 py-2">
              {tokenTotals.map(([agentId, tally]) => (
                <div key={agentId} className="flex flex-wrap items-baseline justify-between gap-x-2 text-micro">
                  <span className="font-medium">
                    {agents.find((a) => a.id === agentId)?.name ?? t("boardArea.components.taskDetail.agentFallback")}
                  </span>
                  <span className="text-muted-foreground">{tokenLine(tally)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </section>
  );
}
