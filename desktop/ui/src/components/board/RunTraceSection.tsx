import {
  ChevronDown,
  ChevronRight,
  Download,
  ExternalLink,
  FileText,
  Globe,
  Pencil,
  SquareKanban,
  Terminal,
  Undo2,
  type LucideIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type RunTraceCategory, type RunTraceEvent, type RunTraceSummary } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useRunTrace } from "@/hooks/useRunTrace";
import { desktopRunner } from "@/lib/desktop-bridge";
import {
  buildTraceHtml,
  detailEffect,
  detailLines,
  detailNumber,
  detailString,
  formatDuration,
  formatSizeDelta,
  readLabel,
  saveTextFile,
  shortSha,
  TRACE_CATEGORIES,
  type TraceHtmlLabels,
} from "@/lib/runTrace";
import { cn } from "@/lib/utils";

const CATEGORY_ICON: Record<RunTraceCategory, LucideIcon> = {
  read: FileText,
  change: Pencil,
  exec: Terminal,
  external: Globe,
  board: SquareKanban,
  undo: Undo2,
};

interface RunTraceSectionProps {
  repositoryId: string;
  taskId: string;
  runId: string;
  live: boolean;
  active: boolean;
  prUrl?: string | null;
}

export function RunTraceSection({ repositoryId, taskId, runId, live, active, prUrl }: RunTraceSectionProps) {
  const { t, lang } = useI18n();
  const trace = useRunTrace(repositoryId, taskId, runId, active, live);
  const [filter, setFilter] = useState<RunTraceCategory | "all">("all");

  const shown = useMemo(
    () => (filter === "all" ? trace.events : trace.events.filter((e) => e.category === filter)),
    [trace.events, filter],
  );

  if (trace.loading && !trace.summary) {
    return (
      <div className="space-y-2" aria-busy="true" aria-label={t("runTrace.loading")}>
        <Skeleton className="h-4 w-2/3" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }
  if (trace.error && !trace.summary) {
    return <Notice variant="error" title={t("runTrace.loadFailed")}>{trace.error}</Notice>;
  }
  if (trace.recorded === false || !trace.summary) {
    return <p className="text-caption text-muted-foreground">{t("runTrace.notRecorded")}</p>;
  }

  const { summary, events } = { summary: trace.summary, events: trace.events };
  const exportBase = `run-${runId.slice(0, 8)}`;

  const htmlLabels = (): TraceHtmlLabels => ({
    lang,
    title: t("runTrace.export.htmlTitle"),
    generated: t("runTrace.export.generated", { date: new Date().toISOString() }),
    counts: headline(summary, t),
    untouched: summary.verification?.available && summary.verification.tracked_files > 0
      ? t("runTrace.untouched", { count: summary.verification.untouched })
      : "",
    verifiedFiles: t("runTrace.files.title"),
    unrecordedTitle: t("runTrace.unrecorded.title"),
    unrecordedBody: t("runTrace.unrecorded.body"),
    nonUndoable: t("runTrace.nonUndoable.title"),
    timeline: t("runTrace.tab.trace"),
    noEvents: t("runTrace.noEvents"),
    commandLabel: t("runTrace.detail.command"),
    exitLabel: t("runTrace.detail.exit", { code: "" }).trim(),
    failed: t("runTrace.row.failed"),
    category: {
      read: t("runTrace.filters.read"),
      change: t("runTrace.filters.change"),
      exec: t("runTrace.filters.exec"),
      external: t("runTrace.filters.external"),
      board: t("runTrace.filters.board"),
      undo: t("runTrace.filters.undo"),
    },
  });

  const exportJson = async () => {
    try {
      const all = await fullEvents(repositoryId, taskId, runId, events);
      saveTextFile(
        `${exportBase}-${t("runTrace.export.jsonFile")}`,
        "application/json",
        JSON.stringify({ exported_at: new Date().toISOString(), summary, events: all }, null, 2),
      );
    } catch {
      toast.error(t("runTrace.export.failed"));
    }
  };

  const exportHtml = async () => {
    try {
      const all = await fullEvents(repositoryId, taskId, runId, events);
      saveTextFile(`${exportBase}-${t("runTrace.export.htmlFile")}`, "text/html", buildTraceHtml(summary, all, htmlLabels()));
    } catch {
      toast.error(t("runTrace.export.failed"));
    }
  };

  const openDiff = (event: React.MouseEvent<HTMLAnchorElement>) => {
    const runner = desktopRunner();
    if (!prUrl || !runner) return;
    event.preventDefault();
    void runner.openExternal(`${prUrl}/files`);
  };

  const v = summary.verification;

  return (
    <div className="min-w-0 space-y-3">
      <div className="space-y-1">
        <p className="text-caption font-medium">{headline(summary, t)}</p>
        {summary.counts.failed > 0 && (
          <p className="text-micro text-muted-foreground">{t("runTrace.failedCalls", { count: summary.counts.failed })}</p>
        )}
        {summary.run.dropped > 0 && (
          <p className="text-micro text-muted-foreground">
            {t("runTrace.droppedCalls", { count: summary.run.dropped, limit: 5000 })}
          </p>
        )}
        {summary.run.base_sha && (
          <p className="text-micro text-muted-foreground">{t("runTrace.startedFrom", { sha: shortSha(summary.run.base_sha) })}</p>
        )}
        {v?.available && v.tracked_files > 0 && (
          <p className="text-micro text-muted-foreground">{t("runTrace.untouched", { count: v.untouched })}</p>
        )}
      </div>

      {v && !v.available && (
        <Notice variant="info" title={t("runTrace.tab.trace")} role={null}>
          {t(`runTrace.unavailable.${["no_checkout", "no_base", "git_failed"].includes(v.reason ?? "") ? v.reason : "other"}`)}
        </Notice>
      )}

      {v && v.unrecorded.length > 0 && (
        <Notice variant="warning" title={t("runTrace.unrecorded.title")} role="status">
          <p>{t("runTrace.unrecorded.body")}</p>
          <ul className="mt-1 list-disc pl-5 font-mono text-micro [overflow-wrap:anywhere]">
            {v.unrecorded.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </Notice>
      )}

      {v && v.recorded_only.length > 0 && (
        <Notice variant="info" title={t("runTrace.recordedOnly.title")} role={null}>
          <p>{t("runTrace.recordedOnly.body")}</p>
          <ul className="mt-1 list-disc pl-5 font-mono text-micro [overflow-wrap:anywhere]">
            {v.recorded_only.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </Notice>
      )}

      {v?.available && (
        <ChangedFiles summary={summary} prUrl={prUrl} onDiff={openDiff} />
      )}

      {summary.non_undoable.length > 0 && (
        <div className="space-y-1 rounded-lg border border-border bg-muted/10 px-3 py-2">
          <p className="text-caption font-medium">{t("runTrace.nonUndoable.title")}</p>
          <p className="text-micro text-muted-foreground">{t("runTrace.nonUndoable.hint")}</p>
          <ul className="space-y-0.5 text-micro">
            {summary.non_undoable.map((x) => (
              <li key={x.seq} className="min-w-0 [overflow-wrap:anywhere]">
                <span className="font-medium">{t(`runTrace.nonUndoable.kinds.${x.kind}`)}</span>
                <span className="text-muted-foreground"> · {x.tool}{x.target ? ` · ${x.target}` : ""}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-1.5">
        {(["all", ...TRACE_CATEGORIES] as const).map((cat) => {
          const count = cat === "all" ? summary.counts.total : summary.counts[cat];
          return (
            <Button
              key={cat}
              type="button"
              size="sm"
              variant={filter === cat ? "secondary" : "ghost"}
              aria-pressed={filter === cat}
              className="h-7 gap-1 px-2 text-caption"
              onClick={() => setFilter(cat)}
            >
              {t(`runTrace.filters.${cat}`)}
              <span className="tabular-nums text-muted-foreground">{count}</span>
            </Button>
          );
        })}
        {summary.counts.undo > 0 && (
          <Button
            type="button"
            size="sm"
            variant={filter === "undo" ? "secondary" : "ghost"}
            aria-pressed={filter === "undo"}
            className="h-7 gap-1 px-2 text-caption"
            onClick={() => setFilter("undo")}
          >
            {t("runTrace.filters.undo")}
            <span className="tabular-nums text-muted-foreground">{summary.counts.undo}</span>
          </Button>
        )}
      </div>

      {shown.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {events.length === 0 ? t("runTrace.noEvents") : t("runTrace.row.emptyFilter")}
        </p>
      ) : (
        <ol className="max-h-[480px] divide-y divide-border overflow-y-auto rounded-lg border border-border">
          {shown.map((event) => (
            <TraceRow key={event.seq} event={event} />
          ))}
        </ol>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => void exportJson()}>
          <Download />
          {t("runTrace.export.json")}
        </Button>
        <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => void exportHtml()}>
          <Download />
          {t("runTrace.export.html")}
        </Button>
      </div>
    </div>
  );
}

function headline(summary: RunTraceSummary, t: (k: string, p?: Record<string, string | number>) => string): string {
  return t("runTrace.headline", {
    read: summary.counts.read_files,
    changed: summary.counts.changed_files,
    ran: summary.counts.exec,
    external: summary.counts.external,
  });
}

async function fullEvents(repositoryId: string, taskId: string, runId: string, held: RunTraceEvent[]): Promise<RunTraceEvent[]> {
  if (held.length > 0) return held;
  const out: RunTraceEvent[] = [];
  let after = 0;
  for (let i = 0; i < 20; i++) {
    const res = await api.listRunTrace(repositoryId, taskId, runId, after);
    out.push(...res.events);
    if (!res.next_after) break;
    after = res.next_after;
  }
  return out;
}

function ChangedFiles({
  summary,
  prUrl,
  onDiff,
}: {
  summary: RunTraceSummary;
  prUrl?: string | null;
  onDiff: (event: React.MouseEvent<HTMLAnchorElement>) => void;
}) {
  const { t } = useI18n();
  const files = summary.verification?.files ?? [];
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between gap-2">
        <p className="text-caption font-medium">{t("runTrace.files.title")}</p>
        {prUrl && files.length > 0 && (
          <a
            href={`${prUrl}/files`}
            target="_blank"
            rel="noreferrer"
            onClick={onDiff}
            className="inline-flex items-center gap-1 text-micro text-primary hover:underline"
          >
            <ExternalLink className="h-3 w-3" />
            {t("runTrace.files.diff")}
          </a>
        )}
      </div>
      {files.length === 0 ? (
        <p className="text-micro text-muted-foreground">{t("runTrace.files.none")}</p>
      ) : (
        <ul className="max-h-40 space-y-0.5 overflow-y-auto font-mono text-micro">
          {files.map((f) => (
            <li key={`${f.status}:${f.path}`} className="flex min-w-0 items-baseline gap-2">
              <Badge variant="outline" className="shrink-0 px-1.5 py-0 text-micro font-normal">
                {t(`runTrace.files.status.${f.status}`)}
              </Badge>
              <span className="min-w-0 [overflow-wrap:anywhere]">{f.from ? `${f.from} → ${f.path}` : f.path}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function TraceRow({ event }: { event: RunTraceEvent }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const Icon = CATEGORY_ICON[event.category] ?? SquareKanban;
  const Chevron = open ? ChevronDown : ChevronRight;
  const target = event.category === "read" ? readLabel(event) : (event.target ?? "");
  const failed = event.status !== "ok";
  const effect = detailEffect(event);
  return (
    <li className="min-w-0">
      <Button
        type="button"
        variant="ghost"
        aria-expanded={open}
        aria-label={`${event.tool} ${target}`}
        className="h-auto w-full min-w-0 items-start justify-start gap-2 whitespace-normal rounded-none px-3 py-1.5 text-left font-normal"
        onClick={() => setOpen((v) => !v)}
      >
        <Chevron className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <Icon className={cn("mt-0.5 h-3.5 w-3.5 shrink-0", failed ? "text-destructive" : "text-muted-foreground")} aria-hidden />
        <span className="min-w-0 flex-1 text-micro">
          <span className="font-medium">{event.tool}</span>
          {target && <span className="ml-1.5 font-mono text-muted-foreground [overflow-wrap:anywhere]">{target}</span>}
          {failed && (
            <Badge variant="destructive" className="ml-1.5 px-1.5 py-0 text-micro">
              {event.status === "unfinished" ? t("runTrace.row.unfinished") : t("runTrace.row.failed")}
            </Badge>
          )}
          {effect && (
            <Badge variant="warning" className="ml-1.5 px-1.5 py-0 text-micro">
              {t(`runTrace.nonUndoable.kinds.${effect}`)}
            </Badge>
          )}
        </span>
        <span className="shrink-0 text-micro tabular-nums text-muted-foreground">{formatDuration(event.duration_ms)}</span>
      </Button>
      {open && <TraceDetail event={event} />}
    </li>
  );
}

function Lines({ lines }: { lines: string[] }) {
  return (
    <pre className="max-h-48 overflow-auto rounded-md bg-muted/40 p-2 font-mono text-micro [overflow-wrap:anywhere] whitespace-pre-wrap">
      {lines.join("\n")}
    </pre>
  );
}

function TraceDetail({ event }: { event: RunTraceEvent }) {
  const { t } = useI18n();
  const lines: React.ReactNode[] = [];
  const meta: string[] = [t(`runTrace.detail.source.${event.source}`)];
  if (event.category === "exec") {
    const exit = detailNumber(event, "exit_code");
    meta.push(exit == null ? t("runTrace.detail.exitUnknown") : t("runTrace.detail.exit", { code: exit }));
    meta.push(`${t("runTrace.detail.cwd")}: ${detailString(event, "cwd") || "."}`);
  }
  if (event.category === "change") {
    const before = detailString(event, "hash_before");
    const after = detailString(event, "hash_after");
    if (before || after) meta.push(t("runTrace.detail.hashes", { before: before.slice(0, 10) || "-", after: after.slice(0, 10) || "-" }));
    const delta = detailNumber(event, "size_delta");
    if (delta != null) meta.push(t("runTrace.detail.sizeDelta", { delta: formatSizeDelta(delta) }));
    if (event.detail?.existed_before === false) meta.push(t("runTrace.detail.created"));
  }
  if (event.category === "external") {
    const host = detailString(event, "host");
    if (host) meta.push(t("runTrace.detail.host", { host }));
    const method = detailString(event, "method");
    if (method) meta.push(t("runTrace.detail.method", { method }));
  }
  const ids = event.detail?.ids ?? event.detail?.result_ids;
  if (ids && typeof ids === "object") {
    meta.push(`${t("runTrace.detail.ids")}: ${Object.values(ids as Record<string, string>).join(", ")}`);
  }

  const command = detailString(event, "command");
  const head = detailLines(event, "output_head");
  const tail = detailLines(event, "output_tail");
  const total = detailNumber(event, "output_lines");
  if (command) {
    lines.push(
      <div key="cmd" className="space-y-1">
        <p className="text-micro font-medium">{t("runTrace.detail.command")}</p>
        <Lines lines={[command]} />
      </div>,
    );
  }
  if (head.length > 0) {
    lines.push(
      <div key="head" className="space-y-1">
        <p className="text-micro font-medium">{t("runTrace.detail.outputHead")}</p>
        <Lines lines={head} />
      </div>,
    );
  }
  if (tail.length > 0) {
    const omitted = total != null ? total - head.length - tail.length : 0;
    lines.push(
      <div key="tail" className="space-y-1">
        {omitted > 0 && <p className="text-micro text-muted-foreground">{t("runTrace.detail.omitted", { count: omitted })}</p>}
        <p className="text-micro font-medium">{t("runTrace.detail.outputTail")}</p>
        <Lines lines={tail} />
      </div>,
    );
  }
  return (
    <div className="space-y-2 px-3 pb-2 pl-9">
      <p className="text-micro text-muted-foreground [overflow-wrap:anywhere]">{meta.join(" · ")}</p>
      {event.error && (
        <div className="space-y-1">
          <p className="text-micro font-medium text-destructive">{t("runTrace.detail.error")}</p>
          <Lines lines={[event.error]} />
        </div>
      )}
      {lines}
    </div>
  );
}
