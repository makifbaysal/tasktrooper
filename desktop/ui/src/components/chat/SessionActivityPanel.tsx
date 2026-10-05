import { Activity, CircleStop, Loader2, PanelRightClose } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import type { SessionRun } from "@/api";
import { ActivityFeed } from "@/components/activity/ActivityFeed";
import { AgentRunHeader } from "@/components/activity/AgentRunHeader";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { useSessionActivity } from "@/hooks/useSessionActivity";
import { useStickToBottom } from "@/hooks/useStickToBottom";
import { buildActivityFeed, isRunLive, type FeedCurrent } from "@/lib/activityFeed";
import { cn } from "@/lib/utils";

const OPEN_KEY = "tt.chat.activityPanel.open";
const INLINE_MIN_WIDTH = 1280;
const EMPTY_STATS = { toolCalls: 0, failures: 0, subagents: 0 };

function readOpen(): boolean {
  try {
    const stored = window.localStorage.getItem(OPEN_KEY);
    if (stored === "1") return true;
    if (stored === "0") return false;
  } catch {
    // storage can be blocked; the width default below still applies
  }
  return window.innerWidth >= INLINE_MIN_WIDTH;
}

function writeOpen(open: boolean) {
  try {
    window.localStorage.setItem(OPEN_KEY, open ? "1" : "0");
  } catch {
    // a remembered panel state is a convenience, never required
  }
}

interface SessionActivityPanelProps {
  sessionId: string;
  agentName: string;
  lead?: boolean;
  /** Session runs as `sessionActivity` returns them: newest first. */
  runs: SessionRun[];
  sending: boolean;
  onStop?: () => void;
  stopping?: boolean;
}

// The parent must be `relative` and a flex row: below xl the open panel overlays
// the chat from the right edge, because beside the app sidebar and the session
// list a fixed-width column left the conversation itself ~100px wide.
export function SessionActivityPanel({
  sessionId,
  agentName,
  lead = false,
  runs,
  sending,
  onStop,
  stopping = false,
}: SessionActivityPanelProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(readOpen);
  const { inputs } = useSessionActivity(runs, open);

  const toggle = (next: boolean) => {
    setOpen(next);
    writeOpen(next);
  };

  const feed = useMemo(() => buildActivityFeed(inputs), [inputs]);
  const newest = inputs[inputs.length - 1];
  const newestFeed = useMemo(() => (newest ? buildActivityFeed([newest]) : null), [newest]);
  const newestRun = runs.reduce<SessionRun | null>(
    (best, run) => (!best || Date.parse(run.started_at) > Date.parse(best.started_at) ? run : best),
    null,
  );

  const live = sending || (newest ? newest.live : false);
  const railLive = sending || runs.some((run) => isRunLive(run.status, [], null));
  const waitingForRun = sending && !newest?.live;
  const startingCurrent: FeedCurrent | null = waitingForRun ? { kind: "starting", since: "" } : null;
  const runStatus = live ? "running" : (newestRun?.status ?? "pending");

  const scrollRef = useRef<HTMLDivElement>(null);
  useStickToBottom(scrollRef, [open, feed.items.length, feed.current?.kind, live, sessionId]);

  const showAside = open;
  const stopButton =
    live && onStop ? (
      <Button type="button" variant="outline" size="sm" className="gap-1.5" disabled={stopping} onClick={onStop}>
        {stopping ? <Loader2 className="animate-spin" /> : <CircleStop />}
        {t("activityArea.header.stop")}
      </Button>
    ) : null;

  return (
    <>
      {showAside && (
        <aside
          aria-label={t("activityArea.panel.title")}
          className={cn(
            "flex h-full min-w-0 flex-col border-l border-border bg-background",
            "w-[380px] max-w-full xl:relative xl:w-[340px] xl:max-w-none xl:shrink-0 2xl:w-[400px]",
            "max-xl:absolute max-xl:inset-y-0 max-xl:right-0 max-xl:z-20 max-xl:shadow-xl",
          )}
        >
          <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-3">
            <Activity aria-hidden className="h-4 w-4 shrink-0 text-muted-foreground" />
            <h2 className="min-w-0 flex-1 truncate text-body font-semibold">{t("activityArea.panel.title")}</h2>
            {live && (
              <span className="flex shrink-0 items-center gap-1.5 text-micro text-muted-foreground">
                <span aria-hidden className="h-2 w-2 animate-pulse rounded-full bg-warning" />
                {t("activityArea.panel.live")}
              </span>
            )}
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-8 w-8 shrink-0"
              aria-label={t("activityArea.panel.collapse")}
              onClick={() => toggle(false)}
            >
              <PanelRightClose />
            </Button>
          </div>

          {(newestRun || waitingForRun) && (
            <div className="shrink-0 border-b border-border p-3">
              <AgentRunHeader
                agentName={agentName}
                lead={lead}
                runStatus={runStatus}
                live={live}
                startedAt={waitingForRun ? undefined : newestRun?.started_at}
                endedAt={waitingForRun ? undefined : newestRun?.completed_at}
                current={waitingForRun ? startingCurrent : (newestFeed?.current ?? null)}
                stats={waitingForRun ? EMPTY_STATS : (newestFeed?.stats ?? EMPTY_STATS)}
                actions={stopButton}
              />
            </div>
          )}

          <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
            <ActivityFeed feed={feed} live={live && !waitingForRun} emptyLabel={t("activityArea.panel.empty")} />
          </div>
        </aside>
      )}

      <div
        className={cn(
          "w-11 shrink-0 flex-col items-center border-l border-border bg-background py-2",
          open ? "hidden max-xl:flex" : "flex",
        )}
      >
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="relative h-8 w-8"
          aria-label={t("activityArea.panel.open")}
          aria-expanded={open}
          onClick={() => toggle(true)}
        >
          <Activity />
          {railLive && (
            <span aria-hidden className="absolute right-1 top-1 h-2 w-2 animate-pulse rounded-full bg-warning" />
          )}
        </Button>
      </div>
    </>
  );
}
