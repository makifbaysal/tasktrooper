import { Terminal } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type LogSeverity, type RuntimeLogEntry } from "@/api";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { usePolling } from "@/hooks/usePolling";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type Range = "15m" | "1h" | "6h" | "24h" | "custom";
const RANGES: Range[] = ["15m", "1h", "6h", "24h", "custom"];
const RANGE_MS: Partial<Record<Range, number>> = { "15m": 900_000, "1h": 3_600_000, "6h": 21_600_000, "24h": 86_400_000 };
// Only these four are offered: a min-severity floor, not a level to isolate —
// "critical" is folded into "error and above" the way the provider adapters read it.
const SEVERITY_OPTIONS: LogSeverity[] = ["debug", "info", "warning", "error"];
const ALL_SEVERITIES = "__all__";

const SEVERITY_CLASS: Record<LogSeverity, string> = {
  debug: "text-muted-foreground",
  info: "text-info",
  warning: "text-warning",
  error: "text-destructive",
  critical: "text-destructive font-semibold",
};

function formatLogTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number, len = 2) => String(n).padStart(len, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

const logKey = (entry: RuntimeLogEntry) => `${entry.timestamp}\u0000${entry.severity}\u0000${entry.message}`;

const PAGE_SIZE = 200;

// Every provider answers newest-first, so a live tick only has lines to add at
// the top. Prepending them — instead of replacing the list with the first page —
// keeps any older pages already loaded, the scroll position and the wrapped rows.
// `null` when the head is a full page sharing no line with what is shown: more
// arrived since the last tick than one page holds, and prepending it would
// leave a silent hole below it, so the caller reloads the first page instead.
export function mergeLiveTail(
  prev: RuntimeLogEntry[] | null,
  head: RuntimeLogEntry[],
  headIsFullPage = false,
): RuntimeLogEntry[] | null {
  const known = new Set((prev ?? []).map(logKey));
  const fresh = head.filter((entry) => !known.has(logKey(entry)));
  if (headIsFullPage && fresh.length === head.length) return null;
  if (!prev) return head;
  return fresh.length === 0 ? prev : [...fresh, ...prev];
}

// Content-based, so a prepended line does not shift every row's key (and with
// it which rows are wrapped); repeats of an identical line get a counter.
function rowKeys(entries: RuntimeLogEntry[]): string[] {
  const seen = new Map<string, number>();
  return entries.map((entry) => {
    const base = logKey(entry);
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return n === 0 ? base : `${base}#${n}`;
  });
}

function requestLine(entry: RuntimeLogEntry): string | undefined {
  if (!entry.method && !entry.path) return undefined;
  return [entry.method, entry.path, entry.status_code].filter(Boolean).join(" ");
}

interface LogsPanelProps {
  envId: string;
  className?: string;
}

/** Live-tailable runtime logs for one environment. */
export function LogsPanel({ envId, className }: LogsPanelProps) {
  const { t } = useI18n();
  const [severity, setSeverity] = useState<LogSeverity | "">("");
  const [range, setRange] = useState<Range>("1h");
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");
  const [queryInput, setQueryInput] = useState("");
  const [query, setQuery] = useState("");
  const [live, setLive] = useState(false);
  const [entries, setEntries] = useState<RuntimeLogEntry[] | null>(null);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [truncated, setTruncated] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [wrapped, setWrapped] = useState<Set<string>>(new Set());
  // Bumped by every read the user asked for and by a live tick that replaced
  // the list; a live tick or a "load more" that started under the previous
  // list must not add its lines to the new one.
  const listVersion = useRef(0);
  const shown = useRef<RuntimeLogEntry[] | null>(null);
  useEffect(() => {
    shown.current = entries;
  }, [entries]);

  useEffect(() => {
    const id = setTimeout(() => setQuery(queryInput), 400);
    return () => clearTimeout(id);
  }, [queryInput]);

  const fetchLogs = useCallback(
    async (opts: { append?: boolean; cursor?: string; live?: boolean } = {}) => {
      if (range === "custom" && (!customFrom || !customTo)) return;
      const since = range === "custom" ? new Date(customFrom).toISOString() : new Date(Date.now() - RANGE_MS[range]!).toISOString();
      const until = range === "custom" ? new Date(customTo).toISOString() : undefined;
      if (opts.append) setLoadingMore(true);
      else if (!opts.live) setLoading(true);
      if (!opts.live) listVersion.current += 1;
      const version = listVersion.current;
      try {
        const page = await api.getEnvironmentLogs(envId, {
          since,
          until,
          min_severity: severity || undefined,
          text: query || undefined,
          limit: PAGE_SIZE,
          cursor: opts.cursor,
        });
        if ((opts.live || opts.append) && version !== listVersion.current) return;
        if (opts.live) {
          const full = page.entries.length >= PAGE_SIZE;
          if (mergeLiveTail(shown.current, page.entries, full) !== null) {
            setEntries((prev) => mergeLiveTail(prev, page.entries) ?? page.entries);
            return;
          }
          listVersion.current += 1;
        }
        setEntries((prev) => (opts.append ? [...(prev ?? []), ...page.entries] : page.entries));
        if (!opts.append) setWrapped(new Set());
        setNextCursor(page.next_cursor);
        setTruncated(Boolean(page.truncated));
      } catch (e) {
        // A background tick keeps the lines already shown; only a read the
        // user asked for is worth a toast.
        if (!opts.live) toast.error(e instanceof Error ? e.message : t("repositoryPage.deploy.runtime.logs.loadFailed"));
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [envId, range, customFrom, customTo, severity, query, t],
  );

  useEffect(() => {
    void fetchLogs();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fetchLogs]);

  usePolling(() => fetchLogs({ live: true }), 5000, live);
  const keys = useMemo(() => (entries ? rowKeys(entries) : []), [entries]);

  const toggleWrap = (key: string) =>
    setWrapped((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <div className={cn("space-y-3", className)}>
      <div className="flex flex-wrap items-end gap-2">
        <div className="space-y-1">
          <Label htmlFor="logs-severity" className="text-micro text-muted-foreground">
            {t("repositoryPage.deploy.runtime.logs.severity")}
          </Label>
          <Select
            value={severity || ALL_SEVERITIES}
            onValueChange={(v) => setSeverity(v === ALL_SEVERITIES ? "" : (v as LogSeverity))}
          >
            <SelectTrigger id="logs-severity" className="h-8 w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_SEVERITIES}>{t("repositoryPage.deploy.runtime.logs.severityAll")}</SelectItem>
              {SEVERITY_OPTIONS.map((s) => (
                <SelectItem key={s} value={s}>
                  {t("repositoryPage.deploy.runtime.logs.severityAtLeast", { severity: t(`repositoryPage.deploy.runtime.logs.severityLabel.${s}`) })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1">
          <Label htmlFor="logs-range" className="text-micro text-muted-foreground">
            {t("repositoryPage.deploy.runtime.logs.timeRange")}
          </Label>
          <Select value={range} onValueChange={(v) => setRange(v as Range)}>
            <SelectTrigger id="logs-range" className="h-8 w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RANGES.map((r) => (
                <SelectItem key={r} value={r}>
                  {t(`repositoryPage.deploy.runtime.logs.range.${r}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {range === "custom" && (
          <>
            <div className="space-y-1">
              <Label className="text-micro text-muted-foreground">{t("repositoryPage.deploy.runtime.logs.from")}</Label>
              <Input
                type="datetime-local"
                className="h-8 w-48"
                value={customFrom}
                onChange={(e) => setCustomFrom(e.target.value)}
              />
            </div>
            <div className="space-y-1">
              <Label className="text-micro text-muted-foreground">{t("repositoryPage.deploy.runtime.logs.to")}</Label>
              <Input
                type="datetime-local"
                className="h-8 w-48"
                value={customTo}
                onChange={(e) => setCustomTo(e.target.value)}
              />
            </div>
          </>
        )}

        <Input
          value={queryInput}
          onChange={(e) => setQueryInput(e.target.value)}
          placeholder={t("repositoryPage.deploy.runtime.logs.searchPlaceholder")}
          className="h-8 w-48"
        />

        <label className="ml-auto flex items-center gap-2 text-caption">
          <Switch checked={live} onCheckedChange={setLive} aria-label={t("repositoryPage.deploy.runtime.logs.live")} />
          {t("repositoryPage.deploy.runtime.logs.live")}
        </label>
      </div>

      {truncated && (
        <Notice variant="warning" title={t("repositoryPage.deploy.runtime.logs.truncatedTitle")}>
          {t("repositoryPage.deploy.runtime.logs.truncatedDesc")}
        </Notice>
      )}

      {loading ? (
        <Skeleton className="h-64 w-full" />
      ) : !entries || entries.length === 0 ? (
        <EmptyState icon={Terminal} title={t("repositoryPage.deploy.runtime.logs.empty")} />
      ) : (
        <ScrollArea className="h-80 rounded-lg border border-border">
          <div className="divide-y divide-border font-mono text-micro">
            {entries.map((entry, i) => {
              const key = keys[i];
              const isWrapped = wrapped.has(key);
              const request = requestLine(entry);
              return (
                <div key={key} className="flex items-start gap-2 px-3 py-1.5">
                  <span className="shrink-0 text-muted-foreground">{formatLogTime(entry.timestamp)}</span>
                  <span className={cn("w-16 shrink-0 uppercase", SEVERITY_CLASS[entry.severity])}>{entry.severity}</span>
                  {entry.source && <span className="shrink-0 text-muted-foreground">{entry.source}</span>}
                  <button
                    type="button"
                    onClick={() => toggleWrap(key)}
                    className={cn("min-w-0 flex-1 text-left", isWrapped ? "whitespace-pre-wrap break-all" : "truncate")}
                  >
                    {request && <span className="text-info">{request} </span>}
                    {entry.message}
                  </button>
                </div>
              );
            })}
          </div>
        </ScrollArea>
      )}

      {nextCursor && (
        <div className="flex justify-center">
          <Button size="sm" variant="outline" disabled={loadingMore} onClick={() => void fetchLogs({ append: true, cursor: nextCursor })}>
            {t("repositoryPage.deploy.runtime.logs.loadMore")}
          </Button>
        </div>
      )}
    </div>
  );
}
