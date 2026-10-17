import { ChevronDown, ChevronRight, Copy, ListTree } from "lucide-react";
import { Fragment, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import type { OpsLogEntry } from "@/api";
import { SeverityPill } from "@/components/operations/logs/SeverityPill";
import { logIdentity, type EnvironmentRef } from "@/components/operations/query/opsQuery";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import { intlLocale } from "@/lib/languages";
import { cn } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-1.5 align-top";

interface LogTableProps {
  entries: OpsLogEntry[];
  environments: Map<string, EnvironmentRef>;
  entities: Map<string, OpsEntity>;
  showEntity: boolean;
  entityHref?: (entity: OpsEntity) => string;
  /** Where a trace id opens: the entity's Traces, or nothing when it has none. */
  traceHref?: (traceId: string, entity: OpsEntity | undefined) => string | undefined;
}

// Log lines carry no id: identical lines at one instant are told apart by
// their position among themselves, which survives prepending and appending.
function rowKeys(entries: OpsLogEntry[]): string[] {
  const seen = new Map<string, number>();
  return entries.map((e) => {
    const id = logIdentity(e);
    const n = seen.get(id) ?? 0;
    seen.set(id, n + 1);
    return `${id}\u0003${n}`;
  });
}

function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-[8rem_minmax(0,1fr)] gap-2">
      <dt className="text-micro font-medium tracking-wide text-muted-foreground uppercase">{label}</dt>
      <dd className="min-w-0 break-words font-mono text-caption">{children}</dd>
    </div>
  );
}

/** The explorer's entries, newest first: one dense line each, opened in place
 * to show the request, trace id and structured fields. */
export function LogTable({ entries, environments, entities, showEntity, entityHref, traceHref }: LogTableProps) {
  const { t, lang } = useI18n();
  const l = (key: string, params?: Record<string, string | number>) => t(`operations.logsExplorer.${key}`, params);
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const keys = useMemo(() => rowKeys(entries), [entries]);
  const time = useMemo(
    () =>
      new Intl.DateTimeFormat(intlLocale(lang), {
        month: "short",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        fractionalSecondDigits: 3,
        hourCycle: "h23",
      }),
    [lang],
  );
  const columns = showEntity ? 4 : 3;

  const toggle = (key: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const copyTrace = async (trace: string) => {
    if (await copyText(trace)) toast.success(l("details.traceCopied"));
    else toast.error(t("common.actionFailed"));
  };

  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[720px] border-collapse text-caption" aria-label={l("listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={cn(TH, "w-52")}>{l("columns.time")}</th>
              <th scope="col" className={cn(TH, "w-24")}>{l("columns.severity")}</th>
              {showEntity && <th scope="col" className={cn(TH, "w-40")}>{l("columns.entity")}</th>}
              <th scope="col" className={TH}>{l("columns.message")}</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry, i) => {
              const key = keys[i];
              const expanded = open.has(key);
              const ref = environments.get(entry.environment_id);
              const entity = ref?.entity ?? entities.get(entry.component_id);
              const at = Date.parse(entry.timestamp);
              const request = [entry.method, entry.path].filter(Boolean).join(" ");
              const fields = entry.fields ? Object.entries(entry.fields).sort(([a], [b]) => a.localeCompare(b)) : [];
              const traceLink = entry.trace_id ? traceHref?.(entry.trace_id, entity) : undefined;
              return (
                <Fragment key={key}>
                  <tr
                    className={cn("border-b border-border/50 hover:bg-muted/30", expanded && "bg-muted/30")}
                    onClick={(ev) => {
                      if ((ev.target as HTMLElement).closest("a,button")) return;
                      // Selecting a line's text to copy it must not fold it shut.
                      if (window.getSelection()?.toString()) return;
                      toggle(key);
                    }}
                  >
                    <td className={cn(TD, "whitespace-nowrap font-mono tabular-nums text-muted-foreground")}>
                      <span className="inline-flex items-center gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="-ml-1 h-5 w-5 shrink-0 [&_svg]:size-3.5"
                          aria-expanded={expanded}
                          aria-label={expanded ? l("collapse") : l("expand")}
                          onClick={() => toggle(key)}
                        >
                          {expanded ? <ChevronDown aria-hidden /> : <ChevronRight aria-hidden />}
                        </Button>
                        <time dateTime={entry.timestamp}>{Number.isFinite(at) ? time.format(at) : entry.timestamp}</time>
                      </span>
                    </td>
                    <td className={TD}>
                      <SeverityPill severity={entry.severity} />
                    </td>
                    {showEntity && (
                      <td className={cn(TD, "max-w-40")}>
                        {entity && entityHref ? (
                          <Link to={entityHref(entity)} className="block truncate hover:underline">
                            {entity.name}
                          </Link>
                        ) : (
                          <span className="block truncate">{entity?.name ?? t("operations.opsQuery.unavailable.unknownEntity")}</span>
                        )}
                      </td>
                    )}
                    <td className={cn(TD, "max-w-0 w-full")}>
                      <p className={cn("font-mono text-caption", expanded ? "whitespace-pre-wrap break-words" : "truncate")}>
                        {entry.status_code ? (
                          <span className={cn("mr-2 tabular-nums", entry.status_code >= 500 ? "text-destructive" : entry.status_code >= 400 ? "text-warning" : "text-muted-foreground")}>
                            {entry.status_code}
                          </span>
                        ) : null}
                        {request && !expanded && <span className="mr-2 text-muted-foreground">{request}</span>}
                        {entry.message}
                      </p>
                    </td>
                  </tr>
                  {expanded && (
                    <tr className="border-b border-border/50 bg-surface-sunken/40">
                      <td colSpan={columns} className="px-10 py-3">
                        <dl className="space-y-1.5">
                          <Detail label={l("details.timestamp")}>{entry.timestamp}</Detail>
                          {request && <Detail label={l("details.request")}>{request}</Detail>}
                          {entry.status_code ? <Detail label={l("details.status")}>{entry.status_code}</Detail> : null}
                          {entry.trace_id && (
                            <Detail label={l("details.trace")}>
                              <span className="inline-flex max-w-full items-center gap-1">
                                <span className="break-all">{entry.trace_id}</span>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-6 w-6 shrink-0 [&_svg]:size-3.5"
                                  aria-label={l("details.copyTrace")}
                                  title={l("details.copyTrace")}
                                  onClick={() => void copyTrace(entry.trace_id ?? "")}
                                >
                                  <Copy aria-hidden />
                                </Button>
                                {traceLink && (
                                  <Link
                                    to={traceLink}
                                    className="inline-flex shrink-0 items-center gap-1 font-sans text-micro font-medium text-info hover:underline"
                                  >
                                    <ListTree className="h-3 w-3" aria-hidden />
                                    {t("operations.telemetry.traces.openFromLog")}
                                  </Link>
                                )}
                              </span>
                            </Detail>
                          )}
                          {entry.source && <Detail label={l("details.source")}>{entry.source}</Detail>}
                          {ref?.resource && <Detail label={l("details.resource")}>{ref.resource}</Detail>}
                          {fields.length > 0 && (
                            <Detail label={l("details.fields")}>
                              <span className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-0.5">
                                {fields.map(([k, v]) => (
                                  <Fragment key={k}>
                                    <span className="text-muted-foreground">{k}</span>
                                    <span className="break-all">{v}</span>
                                  </Fragment>
                                ))}
                              </span>
                            </Detail>
                          )}
                        </dl>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
